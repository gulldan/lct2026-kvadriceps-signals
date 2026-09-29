package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IndexRepo владеет указателями публикации; воркер с истёкшей арендой их не активирует.
type IndexRepo struct{ pool *pgxpool.Pool }

func NewIndexRepo(db *DB) *IndexRepo { return &IndexRepo{pool: db.Pool} }

func (r *IndexRepo) Begin(ctx context.Context, in indexdoc.Lease) (indexdoc.Job, error) {
	if err := in.Validate(); err != nil {
		return indexdoc.Job{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return indexdoc.Job{}, fmt.Errorf("begin index lease: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	job := indexdoc.Job{Lease: in}
	var raw []byte
	var legacyCount int
	err = tx.QueryRow(ctx, `SELECT object_key,content_hash,source_metadata,chunk_count FROM documents
WHERE id=$1 AND owner_id=$2 AND generation=$3 FOR UPDATE`, in.DocumentID, in.OwnerID, max(1, in.Generation)).Scan(
		&job.ObjectKey, &job.ContentHash, &raw, &legacyCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, indexdoc.ErrNotFound
	}
	if err != nil {
		return job, fmt.Errorf("read source index document: %w", err)
	}
	if len(raw) > 0 {
		src, decodeErr := sourcedoc.Decode(raw)
		if decodeErr != nil {
			return job, fmt.Errorf("read index provenance: %w", decodeErr)
		}
		job.Source = *src
	}
	if !indexdoc.IsHash(job.ContentHash) {
		return job, indexdoc.ErrInvalid
	}
	if _, err = tx.Exec(ctx, `INSERT INTO source_index_heads(document_id) VALUES($1) ON CONFLICT DO NOTHING`,
		in.DocumentID); err != nil {
		return job, fmt.Errorf("ensure index head: %w", err)
	}
	var available, revoked, legacyPruned bool
	var activeID *string
	err = tx.QueryRow(ctx, `SELECT (lease_token=$2 OR leased_until<=clock_timestamp()),revoked,legacy_pruned,
active_snapshot_id FROM source_index_heads WHERE document_id=$1 FOR UPDATE`, in.DocumentID, in.Token).Scan(
		&available, &revoked, &legacyPruned, &activeID)
	if err != nil {
		return job, fmt.Errorf("lock source index: %w", err)
	}
	if revoked {
		return job, indexdoc.ErrNotFound
	}
	if !available {
		return job, indexdoc.ErrConflict
	}
	if activeID != nil {
		job.Active, err = readIndexManifest(ctx, tx, in.DocumentID, *activeID, true)
		if err != nil {
			return job, err
		}
	}
	job.Previous, err = previousIndexes(ctx, tx, in.DocumentID, in.Token)
	if err != nil {
		return job, err
	}
	if !legacyPruned && legacyCount > 0 {
		job.Previous = append(job.Previous, indexdoc.Previous{ChunkCount: legacyCount})
	}
	err = tx.QueryRow(ctx, `UPDATE source_index_heads SET lease_token=$2,
leased_until=clock_timestamp()+interval '2 minutes',last_error='',updated_at=clock_timestamp()
WHERE document_id=$1 RETURNING leased_until`, in.DocumentID, in.Token).Scan(&job.LeasedUntil)
	if err != nil {
		return job, fmt.Errorf("acquire source index lease: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return job, fmt.Errorf("commit source index lease: %w", err)
	}
	return job, nil
}

func previousIndexes(ctx context.Context, tx pgx.Tx, docID, token string) ([]indexdoc.Previous, error) {
	rows, err := tx.Query(ctx, `SELECT snapshot_id,(manifest->>'chunk_count')::integer FROM source_index_snapshots
WHERE document_id=$1 AND snapshot_id<>$2 AND NOT pruned ORDER BY created_at DESC LIMIT 32`, docID, token)
	if err != nil {
		return nil, fmt.Errorf("list previous source indexes: %w", err)
	}
	defer rows.Close()
	var previous []indexdoc.Previous
	for rows.Next() {
		var p indexdoc.Previous
		if err = rows.Scan(&p.SnapshotID, &p.ChunkCount); err != nil {
			return nil, fmt.Errorf("scan previous index: %w", err)
		}
		previous = append(previous, p)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read previous indexes: %w", err)
	}
	return previous, nil
}

func readIndexManifest(ctx context.Context, tx pgx.Tx, docID, snapshotID string, ready bool) (*indexdoc.Manifest, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT manifest FROM source_index_snapshots WHERE document_id=$1 AND snapshot_id=$2
AND (ready OR NOT $3)`, docID, snapshotID, ready).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, indexdoc.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read source snapshot: %w", err)
	}
	manifest, err := indexdoc.Decode[indexdoc.Manifest](string(raw))
	if err != nil {
		return nil, err
	}
	if err = manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// Renew продлевает только живую аренду того же владельца.
func (r *IndexRepo) Renew(ctx context.Context, in indexdoc.Lease) (indexdoc.Empty, error) {
	if err := in.Validate(); err != nil {
		return indexdoc.Empty{}, err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE source_index_heads h SET leased_until=clock_timestamp()+interval '2 minutes'
FROM documents d WHERE h.document_id=$1 AND d.id=h.document_id AND d.owner_id=$2
AND lease_token=$3 AND leased_until>clock_timestamp() AND NOT revoked AND d.generation=$4`,
		in.DocumentID, in.OwnerID, in.Token, max(1, in.Generation))
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("renew source index: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return indexdoc.Empty{}, indexdoc.ErrConflict
	}
	return indexdoc.Empty{}, nil
}

// lockIndex — общий замок подготовки, активации и финального освобождения.
func lockIndex(ctx context.Context, tx pgx.Tx, in indexdoc.Lease) error {
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT generation FROM documents WHERE id=$1 AND owner_id=$2 FOR UPDATE`,
		in.DocumentID, in.OwnerID).Scan(&generation); err != nil {
		return indexdoc.ErrNotFound
	}
	if generation != max(1, in.Generation) {
		return indexdoc.ErrConflict
	}
	var until time.Time
	var valid bool
	err := tx.QueryRow(ctx, `SELECT h.leased_until,
(h.lease_token=$3 AND h.leased_until>clock_timestamp() AND NOT h.revoked AND d.generation=$4)
FROM source_index_heads h JOIN documents d ON d.id=h.document_id
WHERE h.document_id=$1 AND d.owner_id=$2 FOR UPDATE OF h`,
		in.DocumentID, in.OwnerID, in.Token, max(1, in.Generation)).Scan(
		&until,
		&valid,
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !valid) {
		return indexdoc.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("lock index lease: %w", err)
	}
	return nil
}
