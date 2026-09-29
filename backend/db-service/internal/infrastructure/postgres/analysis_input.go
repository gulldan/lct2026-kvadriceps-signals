package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AnalysisRepo замораживает вход одним снимком вместе с ACL и головами источников.
type AnalysisRepo struct {
	pool    *pgxpool.Pool
	objects analysisdoc.ObjectStore
}

func NewAnalysisRepo(db *DB) *AnalysisRepo { return &AnalysisRepo{pool: db.Pool} }

// NewAnalysisJobs дополнительно проверяет неизменяемые объекты S3 до активации.
func NewAnalysisJobs(db *DB, objects analysisdoc.ObjectStore) *AnalysisRepo {
	return &AnalysisRepo{pool: db.Pool, objects: objects}
}

// Freeze по одному ключу идемпотентности отдаёт исходные байты даже после новых загрузок.
func (r *AnalysisRepo) Freeze(ctx context.Context, request analysisdoc.Request) (analysisdoc.Frozen, error) {
	for range 3 {
		frozen, err := r.freeze(ctx, request)
		if err == nil && r.objects != nil {
			// Запись вне транзакции: повтор чинит отсутствующий объект после сбоя.
			if err = r.objects.Put(ctx, frozen.Artifact, strings.NewReader(frozen.JSON),
				int64(frozen.Bytes), "application/json"); err != nil {
				return analysisdoc.Frozen{}, fmt.Errorf("publish frozen input: %w", err)
			}
			if err = analysisdoc.VerifyObject(ctx, r.objects, frozen.Artifact, frozen.JSON); err != nil {
				return analysisdoc.Frozen{}, err
			}
			return r.Read(ctx, analysisdoc.Read{RunID: frozen.RunID, ReaderID: request.OwnerID, SharedCorpus: request.SharedCorpus})
		}
		pgerr, ok := errors.AsType[*pgconn.PgError](err)
		if !ok || (pgerr.Code != "40001" && pgerr.Code != "23505") {
			return frozen, err
		}
	}
	return analysisdoc.Frozen{}, analysisdoc.ErrConflict
}

func (r *AnalysisRepo) freeze(ctx context.Context, request analysisdoc.Request) (analysisdoc.Frozen, error) {
	request, err := request.Normalize(time.Now().UTC())
	if err != nil {
		return analysisdoc.Frozen{}, err
	}
	// Serializable убирает гонку за потолок истории; Freeze повторяет попытку при откате SSI.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("begin analysis snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	requestRaw, err := json.Marshal(request, json.Deterministic(true))
	if err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("encode analysis request: %w", err)
	}
	requestHash := indexdoc.Hash(requestRaw)
	previous, err := existingAnalysis(ctx, tx, request, requestHash)
	if err == nil {
		return previous, checkAnalysisAccess(ctx, tx, previous.RunID, request.OwnerID, request.SharedCorpus)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return analysisdoc.Frozen{}, err
	}
	if err = analysisInputCapacity(ctx, tx, request); err != nil {
		return analysisdoc.Frozen{}, err
	}
	if err = analysisCollectionScopes(ctx, tx, request); err != nil {
		return analysisdoc.Frozen{}, err
	}
	input := analysisdoc.Input{Version: analysisdoc.Version, RunID: uuid.NewV4().String(), Request: request,
		CreatedAt: time.Now().UTC(), Records: []analysisdoc.Record{}, Excluded: []analysisdoc.Exclusion{},
		Pages: []analysisdoc.Page{}, Limitations: []string{"bounded_provider_search; not_a_census",
			"current_model_reconstruction; no_saved_historical_cluster_memberships"}}
	if err = tx.QueryRow(ctx, `SELECT value FROM source_corpus_epoch WHERE id=true`).Scan(&input.AccessEpoch); err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("read analysis access epoch: %w", err)
	}
	if err = analysisRecords(ctx, tx, &input); err != nil {
		return analysisdoc.Frozen{}, err
	}
	if err = analysisPages(ctx, tx, &input); err != nil {
		return analysisdoc.Frozen{}, err
	}
	frozen, err := saveAnalysis(ctx, tx, input, requestHash)
	if err != nil {
		return frozen, err
	}
	if err = tx.Commit(ctx); err != nil {
		return frozen, fmt.Errorf("commit frozen analysis: %w", err)
	}
	return frozen, nil
}

func existingAnalysis(ctx context.Context, tx pgx.Tx, request analysisdoc.Request, hash string) (analysisdoc.Frozen, error) {
	var frozen analysisdoc.Frozen
	var storedHash string
	err := tx.QueryRow(ctx, `SELECT run_id,manifest,manifest_hash,request_hash FROM signal_analysis_inputs
WHERE owner_id=$1 AND request_id=$2`, request.OwnerID, request.RequestID).Scan(
		&frozen.RunID, &frozen.JSON, &frozen.SHA256, &storedHash)
	if err != nil {
		return frozen, fmt.Errorf("read previous analysis: %w", err)
	}
	if hash != storedHash {
		if err = checkFrozenRequest(request, frozen.JSON, storedHash); err != nil {
			return analysisdoc.Frozen{}, err
		}
	}
	return finishFrozen(frozen)
}

func checkFrozenRequest(request analysisdoc.Request, manifest, storedHash string) error {
	var header struct {
		Request analysisdoc.Request `json:"request"`
	}
	if err := json.Unmarshal([]byte(manifest), &header); err != nil {
		return fmt.Errorf("decode frozen analysis request: %w", err)
	}
	// Расширение запроса генерируется моделью: повтор сохраняет первый набор
	// терминов, но любое изменение пользовательского входа остаётся конфликтом.
	request.SearchTerms = header.Request.SearchTerms
	raw, err := json.Marshal(request, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("encode repeated analysis request: %w", err)
	}
	if indexdoc.Hash(raw) != storedHash {
		return analysisdoc.ErrConflict
	}
	return nil
}

func saveAnalysis(ctx context.Context, tx pgx.Tx, input analysisdoc.Input, requestHash string) (analysisdoc.Frozen, error) {
	raw, err := json.Marshal(input, json.Deterministic(true))
	if err != nil {
		return analysisdoc.Frozen{}, analysisdoc.ErrInvalid
	}
	if len(raw) > analysisdoc.MaxBytes {
		return analysisdoc.Frozen{}, fmt.Errorf("frozen input exceeds %d bytes: %w", analysisdoc.MaxBytes, analysisdoc.ErrInvalid)
	}
	frozen := analysisdoc.Frozen{RunID: input.RunID, JSON: string(raw), SHA256: indexdoc.Hash(raw)}
	_, err = tx.Exec(ctx, `INSERT INTO signal_analysis_inputs
(run_id,owner_id,request_id,request_hash,manifest,manifest_hash) VALUES($1,$2,$3,$4,$5,$6)`,
		input.RunID, input.Request.OwnerID, input.Request.RequestID, requestHash, frozen.JSON, frozen.SHA256)
	if err != nil {
		return frozen, fmt.Errorf("save immutable analysis input: %w", err)
	}
	rows := make([][]any, 0, len(input.Records))
	for _, record := range input.Records {
		rows = append(rows, []any{input.RunID, record.ID, record.Index.SnapshotID})
	}
	for _, excluded := range input.Excluded {
		rows = append(rows, []any{input.RunID, excluded.DocumentID, ""})
	}
	if len(rows) > 0 {
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"signal_analysis_sources"},
			[]string{"run_id", "document_id", "snapshot_id"}, pgx.CopyFromRows(rows))
		if err != nil {
			return frozen, fmt.Errorf("save frozen source membership: %w", err)
		}
	}
	return finishFrozen(frozen)
}

func finishFrozen(frozen analysisdoc.Frozen) (analysisdoc.Frozen, error) {
	if len(frozen.JSON) == 0 || len(frozen.JSON) > analysisdoc.MaxBytes || indexdoc.Hash([]byte(frozen.JSON)) != frozen.SHA256 {
		return analysisdoc.Frozen{}, errors.New("stored analysis manifest hash mismatch")
	}
	var header struct {
		Request struct {
			OwnerID      string `json:"owner_id"`
			SharedCorpus bool   `json:"shared_corpus"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(frozen.JSON), &header); err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("decode frozen input owner: %w", err)
	}
	frozen.Status = analysisdoc.Prepared
	frozen.Artifact = analysisdoc.ArtifactKey(frozen.RunID, frozen.SHA256)
	frozen.Bytes = len(frozen.JSON)
	frozen.ReaderID, frozen.SharedCorpus = header.Request.OwnerID, header.Request.SharedCorpus
	return frozen, nil
}
