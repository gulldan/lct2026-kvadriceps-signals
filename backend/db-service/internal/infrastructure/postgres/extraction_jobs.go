package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/example/db-service/internal/platform/extractdoc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const extractionColumns = `id, source_revision_id, owner_id, source_revision_hash, parser_version,
extractor_version, schema_version, state, attempt, reason_code, input_key, input_sha256,
intermediate_key, intermediate_sha256, output_key, output_sha256, lease_until, created_at, updated_at`

// ExtractionRepo ограждает задания извлечения, чтобы за каждый ключ платить один раз.
type ExtractionRepo struct{ pool *pgxpool.Pool }

func NewExtractionRepo(db *DB) *ExtractionRepo { return &ExtractionRepo{pool: db.Pool} }

func scanExtraction(row pgx.Row) (extractdoc.Job, error) {
	var job extractdoc.Job
	if err := row.Scan(&job.ID, &job.SourceRevisionID, &job.OwnerID, &job.Key.RevisionHash, &job.Key.ParserVersion,
		&job.Key.ExtractorVersion, &job.Key.SchemaVersion, &job.State, &job.Attempt, &job.ReasonCode,
		&job.Input.Key, &job.Input.SHA256, &job.Intermediate.Key, &job.Intermediate.SHA256,
		&job.Output.Key, &job.Output.SHA256, &job.LeasedUntil, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return job, fmt.Errorf("read extraction job row: %w", err)
	}
	return job, nil
}

// Submit идемпотентен по ключу; завершённое задание возвращается без вызова модели.
func (r *ExtractionRepo) Submit(ctx context.Context, in extractdoc.Job) (extractdoc.Job, error) {
	if err := in.Key.Validate(); err != nil {
		return extractdoc.Job{}, err
	}
	if _, err := uuid.Parse(in.SourceRevisionID); err != nil || in.OwnerID == "" ||
		in.Input.Validate() != nil {
		return extractdoc.Job{}, extractdoc.ErrInvalid
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO technology_extraction_jobs
(id, source_revision_id, owner_id, source_revision_hash, parser_version, extractor_version, schema_version,
 input_key, input_sha256)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (id) DO UPDATE SET id=technology_extraction_jobs.id
RETURNING `+extractionColumns,
		in.Key.ID(), in.SourceRevisionID, in.OwnerID, in.Key.RevisionHash, in.Key.ParserVersion,
		in.Key.ExtractorVersion, in.Key.SchemaVersion, in.Input.Key, in.Input.SHA256)
	job, err := scanExtraction(row)
	if err != nil {
		return extractdoc.Job{}, fmt.Errorf("submit extraction job: %w", err)
	}
	return job, nil
}

func (r *ExtractionRepo) Read(ctx context.Context, key extractdoc.Key) (extractdoc.Job, error) {
	if err := key.Validate(); err != nil {
		return extractdoc.Job{}, err
	}
	job, err := scanExtraction(r.pool.QueryRow(ctx,
		`SELECT `+extractionColumns+` FROM technology_extraction_jobs WHERE id=$1`, key.ID()))
	if errors.Is(err, pgx.ErrNoRows) {
		return extractdoc.Job{}, extractdoc.ErrInvalid
	}
	if err != nil {
		return extractdoc.Job{}, fmt.Errorf("read extraction job: %w", err)
	}
	return job, nil
}

func (r *ExtractionRepo) Pending(ctx context.Context, extractor, schema string, limit int,
) ([]extractdoc.Candidate, error) {
	if extractor == "" || schema == "" || limit < 1 || limit > 500 {
		return nil, extractdoc.ErrInvalid
	}
	rows, err := r.pool.Query(ctx, `SELECT d.id, d.owner_id, d.source_metadata::text, s.manifest::text
FROM documents d
JOIN source_index_heads h ON h.document_id = d.id AND h.active_snapshot_id IS NOT NULL AND NOT h.revoked
JOIN source_index_snapshots s ON s.snapshot_id = h.active_snapshot_id AND s.ready
LEFT JOIN technology_extraction_jobs j
  ON j.source_revision_id = d.id AND j.extractor_version = $1 AND j.schema_version = $2
WHERE d.source_metadata IS NOT NULL
  AND j.id IS NULL
ORDER BY d.created_at, d.id LIMIT $3`, extractor, schema, limit)
	if err != nil {
		return nil, fmt.Errorf("list extractable revisions: %w", err)
	}
	defer rows.Close()
	out := []extractdoc.Candidate{}
	for rows.Next() {
		var c extractdoc.Candidate
		if err = rows.Scan(&c.DocumentID, &c.OwnerID, &c.SourceMetadata, &c.Manifest); err != nil {
			return nil, fmt.Errorf("read extractable revision: %w", err)
		}
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan extractable revisions: %w", err)
	}
	return out, nil
}

// Claim перехватывает истёкшие аренды и проваливает задания с исчерпанными попытками.
func (r *ExtractionRepo) Claim(ctx context.Context, extractor, schema, workerID string,
) (extractdoc.Job, extractdoc.Lease, error) {
	if _, err := uuid.Parse(workerID); err != nil || extractor == "" || schema == "" {
		return extractdoc.Job{}, extractdoc.Lease{}, extractdoc.ErrInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return extractdoc.Job{}, extractdoc.Lease{}, fmt.Errorf("begin extraction claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE technology_extraction_jobs
SET state='failed', reason_code='attempts_exhausted', lease_token='', lease_until=NULL, updated_at=now()
WHERE extractor_version=$1 AND schema_version=$2 AND state='running' AND attempt>=$3 AND lease_until<now()`,
		extractor, schema, extractdoc.MaxAttempts); err != nil {
		return extractdoc.Job{}, extractdoc.Lease{}, fmt.Errorf("expire extraction attempts: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM technology_extraction_jobs
WHERE extractor_version=$1 AND schema_version=$2 AND attempt<$3
AND (state='queued' OR (state='running' AND lease_until<now()))
ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED`, extractor, schema, extractdoc.MaxAttempts).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return extractdoc.Job{}, extractdoc.Lease{}, fmt.Errorf("commit empty extraction queue: %w", err)
		}
		return extractdoc.Job{}, extractdoc.Lease{}, nil
	}
	if err != nil {
		return extractdoc.Job{}, extractdoc.Lease{}, fmt.Errorf("claim extraction job: %w", err)
	}
	token := uuid.NewV4().String()
	job, err := scanExtraction(tx.QueryRow(ctx, `UPDATE technology_extraction_jobs
SET state='running', attempt=attempt+1, lease_token=$2, lease_until=now()+$3::interval, reason_code='', updated_at=now()
WHERE id=$1 RETURNING `+extractionColumns, id, token, extractdoc.LeaseDuration.String()))
	if err != nil {
		return extractdoc.Job{}, extractdoc.Lease{}, fmt.Errorf("lease extraction job: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return extractdoc.Job{}, extractdoc.Lease{}, fmt.Errorf("commit extraction claim: %w", err)
	}
	return job, extractdoc.Lease{JobID: job.ID, OwnerID: job.OwnerID, Token: token}, nil
}

// Renew продлевает только живую аренду; устаревший токен отклоняется.
func (r *ExtractionRepo) Renew(ctx context.Context, lease extractdoc.Lease) (time.Time, error) {
	var until time.Time
	err := r.pool.QueryRow(ctx, `UPDATE technology_extraction_jobs
SET lease_until=now()+$4::interval, updated_at=now()
WHERE id=$1 AND owner_id=$2 AND lease_token=$3 AND state='running' AND lease_until>now()
RETURNING lease_until`, lease.JobID, lease.OwnerID, lease.Token, extractdoc.LeaseDuration.String()).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, extractdoc.ErrConflict
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("renew extraction lease: %w", err)
	}
	return until, nil
}

// Finish публикует только под текущей арендой; отменённое задание остаётся завершённым.
func (r *ExtractionRepo) Finish(ctx context.Context, in extractdoc.Result) (extractdoc.Job, error) {
	if err := in.Validate(); err != nil {
		return extractdoc.Job{}, err
	}
	job, err := scanExtraction(r.pool.QueryRow(ctx, `UPDATE technology_extraction_jobs
SET state=$4, reason_code=$5, intermediate_key=$6, intermediate_sha256=$7, output_key=$8, output_sha256=$9,
    lease_token='', lease_until=NULL, updated_at=now()
WHERE id=$1 AND owner_id=$2 AND lease_token=$3 AND state='running' AND lease_until>now()
RETURNING `+extractionColumns,
		in.Lease.JobID, in.Lease.OwnerID, in.Lease.Token, in.State, in.ReasonCode,
		in.Intermediate.Key, in.Intermediate.SHA256, in.Output.Key, in.Output.SHA256))
	if errors.Is(err, pgx.ErrNoRows) {
		return extractdoc.Job{}, extractdoc.ErrConflict
	}
	if err != nil {
		return extractdoc.Job{}, fmt.Errorf("finish extraction job: %w", err)
	}
	return job, nil
}

// Cancel завершает задание без результата, поэтому оно никогда не считается проверенным.
func (r *ExtractionRepo) Cancel(ctx context.Context, id, ownerID, reason string) (extractdoc.Job, error) {
	if id == "" || ownerID == "" || len(reason) > 128 {
		return extractdoc.Job{}, extractdoc.ErrInvalid
	}
	job, err := scanExtraction(r.pool.QueryRow(ctx, `UPDATE technology_extraction_jobs
SET state='cancelled', reason_code=$3, lease_token='', lease_until=NULL, updated_at=now()
WHERE id=$1 AND owner_id=$2 AND state IN ('queued','running')
RETURNING `+extractionColumns, id, ownerID, reason))
	if errors.Is(err, pgx.ErrNoRows) {
		return extractdoc.Job{}, extractdoc.ErrConflict
	}
	if err != nil {
		return extractdoc.Job{}, fmt.Errorf("cancel extraction job: %w", err)
	}
	return job, nil
}
