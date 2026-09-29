package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/example/db-service/internal/platform/harvest"
	"github.com/jackc/pgx/v5"
)

const statusComplete = "complete"

// CommitPage атомарно сохраняет неизменяемые записи, ожидающую отправку и следующий курсор.
func (r *HarvestRepo) CommitPage(ctx context.Context, in harvest.CommitPage) (harvest.Job, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	j, err := lockHarvestLease(ctx, tx, in.Lease)
	if err != nil {
		return j, collectionStorageError(err)
	}
	var priorHash string
	err = tx.QueryRow(
		ctx,
		`
 SELECT manifest_hash FROM source_collection_pages WHERE scope_id=$1 AND run_id=$2 AND page_number=$3
`,
		in.Lease.ScopeID,
		in.Lease.RunID,
		in.PageNumber,
	).Scan(
		&priorHash,
	)
	if err == nil {
		if priorHash != in.ManifestHash {
			return j, harvest.ErrConflict
		}
		return j, collectionStorageError(tx.Commit(ctx))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return j, collectionStorageError(err)
	}
	if j.Exhausted || j.Cursor != in.Cursor || j.PageNumber != in.PageNumber {
		return j, harvest.ErrConflict
	}
	if err = in.Validate(j); err != nil {
		return j, fmt.Errorf("%w: %w", harvest.ErrInvalid, err)
	}
	var replay bool
	err = tx.QueryRow(
		ctx,
		`
 SELECT EXISTS(SELECT 1 FROM source_collection_pages WHERE scope_id=$1 AND run_id=$2 AND raw_hash=$3)
`,
		in.Lease.ScopeID,
		in.Lease.RunID,
		in.RawHash,
	).Scan(
		&replay,
	)
	if err != nil {
		return j, collectionStorageError(err)
	}
	if replay {
		return j, fmt.Errorf("%w: provider replayed a page", harvest.ErrConflict)
	}
	for _, record := range in.Records {
		if err = saveHarvestDocument(ctx, tx, j, record); err != nil {
			return j, collectionStorageError(err)
		}
	}
	if err = saveHarvestPage(ctx, tx, in); err != nil {
		return j, err
	}
	if err = saveCollectionManifest(ctx, tx, j, in); err != nil {
		return j, err
	}
	_, err = tx.Exec(
		ctx,
		`
 UPDATE source_collection_scopes SET cursor=$2, received=received+$3, page_number=page_number+1,
exhausted=$4, incomplete=incomplete OR $5, updated_at=now() WHERE id=$1
`,
		j.Lease.ScopeID,
		in.Next,
		in.Received,
		in.Exhausted,
		in.Rejected > 0 || len(in.Limitations) > 0,
	)
	if err != nil {
		return j, collectionStorageError(err)
	}
	j, err = readHarvestJob(ctx, tx, j.Lease.ScopeID)
	if err != nil {
		return j, collectionStorageError(err)
	}
	return j, collectionStorageError(tx.Commit(ctx))
}

func saveHarvestDocument(ctx context.Context, tx pgx.Tx, job harvest.Job, in harvest.Prepared) error {
	key := harvest.DocumentKey(job.OwnerID, in)
	existing, err := existingHarvestDocument(ctx, tx, job.OwnerID, key, in)
	if err == nil {
		return saveHarvestMembership(ctx, tx, job.Lease.ScopeID, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return collectionStorageError(err)
	}
	metadata, err := json.Marshal(in.Source)
	if err != nil {
		return collectionStorageError(err)
	}
	ext := ".txt"
	if in.MIME == "application/pdf" {
		ext = ".pdf"
	}
	filename := in.Title + ext
	// Дата публикации берётся из паспорта сразу при записи: радар и карта тем
	// читают колонку, а разбор файла заполняет её не у всех документов.
	published := ""
	if in.Source.Publication != nil {
		published = in.Source.Publication.Value
	}
	_, err = tx.Exec(
		ctx,
		`
 INSERT INTO documents(id, owner_id, filename, mime_type, size, object_key, status, content_hash, title,
source_metadata, published_at) VALUES($1, $2, $3, $4, $5, $6, 'uploaded', $7, $8, $9, $10)
`,
		in.DocumentID,
		job.OwnerID,
		filename,
		in.MIME,
		in.Size,
		in.ObjectKey,
		in.BLAKE3,
		in.Title,
		metadata,
		published,
	)
	if err != nil {
		return collectionStorageError(err)
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO source_collection_documents(record_key,document_id,source_hash) VALUES($1,$2,$3)`,
		key,
		in.DocumentID,
		in.Source.ContentHash,
	)
	if err != nil {
		return collectionStorageError(err)
	}
	payload, err := json.Marshal(
		harvest.Dispatch{
			DocumentID: in.DocumentID,
			OwnerID:    job.OwnerID,
			Filename:   filename,
			ObjectKey:  in.ObjectKey,
			MIME:       in.MIME,
			Size:       in.Size,
			Text:       in.Text,
		},
	)
	if err != nil {
		return collectionStorageError(err)
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO source_collection_dispatch(document_id,scope_id,payload) VALUES($1,$2,$3)`,
		in.DocumentID,
		job.Lease.ScopeID,
		payload,
	)
	if err != nil {
		return collectionStorageError(err)
	}
	return saveHarvestMembership(ctx, tx, job.Lease.ScopeID, in.DocumentID)
}

// existingHarvestDocument сопоставляет старые изменяемые хеши ревизий Crossref.
func existingHarvestDocument(ctx context.Context, tx pgx.Tx, owner, key string, in harvest.Prepared) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT document_id FROM source_collection_documents WHERE record_key=$1`, key).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) || in.Source.Provider != "crossref" {
		return id, collectionStorageError(err)
	}
	publication, err := json.Marshal(in.Source.Publication)
	if err != nil {
		return "", collectionStorageError(err)
	}
	err = tx.QueryRow(ctx, `SELECT id FROM documents WHERE owner_id=$1 AND mime_type=$2
AND source_metadata->>'provider'='crossref' AND source_metadata->>'native_id'=$3
AND source_metadata->>'content_hash'=$4 AND source_metadata->>'access_scope'=$5
AND coalesce(source_metadata->>'test_run_id','')=$6
AND coalesce(source_metadata->'publication','null'::jsonb)=$7::jsonb
ORDER BY (status='indexed') DESC, created_at, id LIMIT 1`, owner, in.MIME, in.Source.NativeID,
		in.Source.ContentHash, in.Source.AccessScope, in.Source.TestRunID, publication).Scan(&id)
	if err != nil {
		return "", collectionStorageError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO source_collection_documents(record_key,document_id,source_hash)
VALUES($1,$2,$3)`, key, id, in.Source.ContentHash)
	return id, collectionStorageError(err)
}

func saveHarvestMembership(ctx context.Context, tx pgx.Tx, scopeID, documentID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO source_collection_membership(scope_id,document_id)
VALUES($1,$2) ON CONFLICT(scope_id,document_id) DO NOTHING`, scopeID, documentID)
	return collectionStorageError(err)
}

// saveCollectionManifest оставляет прогон с отказами или ограничениями частичным.
func saveCollectionManifest(ctx context.Context, tx pgx.Tx, job harvest.Job, in harvest.CommitPage) error {
	scope, err := json.Marshal(job.Scope)
	if err != nil {
		return collectionStorageError(err)
	}
	limitations, err := json.Marshal(in.Limitations)
	if err != nil {
		return collectionStorageError(err)
	}
	incomplete := in.Rejected > 0 || len(in.Limitations) > 0
	completeness := "partial"
	if in.Exhausted && !incomplete {
		completeness = statusComplete
	}
	// Received уже включает отклонённые записи поставщика; повторный их учёт завышал бы охват
	// на каждой странице с невалидной записью.
	_, err = tx.Exec(ctx, `
INSERT INTO collection_manifests(scope_id,run_id,owner_id,provider,scope,run_limit,cursor,pages,
listed,received,rejected,processed,limitations,exhausted,incomplete,completeness)
VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (scope_id,run_id) DO UPDATE SET
cursor=excluded.cursor,
pages=collection_manifests.pages+1,
listed=collection_manifests.listed+excluded.listed,
received=collection_manifests.received+excluded.received,
rejected=collection_manifests.rejected+excluded.rejected,
processed=collection_manifests.processed+excluded.processed,
limitations=collection_manifests.limitations||excluded.limitations,
exhausted=excluded.exhausted,
incomplete=collection_manifests.incomplete OR excluded.incomplete,
completeness=CASE WHEN excluded.exhausted AND NOT (collection_manifests.incomplete OR excluded.incomplete)
             THEN 'complete' ELSE 'partial' END,
updated_at=now()`,
		in.Lease.ScopeID, in.Lease.RunID, job.OwnerID, job.Provider, scope, job.Limit, in.Next,
		in.Received, in.Received, in.Rejected, len(in.Records), limitations,
		in.Exhausted, incomplete, completeness)
	if err != nil {
		return collectionStorageError(err)
	}
	return nil
}

func saveHarvestPage(ctx context.Context, tx pgx.Tx, in harvest.CommitPage) error {
	observations, err := json.Marshal(in.Records)
	if err != nil {
		return collectionStorageError(err)
	}
	limitations, err := json.Marshal(in.Limitations)
	if err != nil {
		return collectionStorageError(err)
	}
	_, err = tx.Exec(
		ctx,
		`
 INSERT INTO source_collection_pages(scope_id, run_id, page_number, cursor, next_cursor, exhausted,
received, rejected, raw_key, raw_hash, observations, limitations, manifest_key, manifest_hash) VALUES($1,
$2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
`,
		in.Lease.ScopeID,
		in.Lease.RunID,
		in.PageNumber,
		in.Cursor,
		in.Next,
		in.Exhausted,
		in.Received,
		in.Rejected,
		in.RawKey,
		in.RawHash,
		observations,
		limitations,
		in.ManifestKey,
		in.ManifestHash,
	)
	if err != nil {
		return collectionStorageError(err)
	}

	return nil
}
