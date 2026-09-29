package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/jackc/pgx/v5"
)

// SetAccess меняет доступ и сдвигает эпоху кэша атомарно.
func (r *IndexRepo) SetAccess(ctx context.Context, in indexdoc.AccessChange) (indexdoc.Empty, error) {
	lease := indexdoc.Lease{DocumentID: in.DocumentID, OwnerID: in.OwnerID, Token: in.RequestID}
	if err := lease.Validate(); err != nil {
		return indexdoc.Empty{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("begin source access change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM documents WHERE id=$1 AND owner_id=$2
AND source_metadata IS NOT NULL FOR UPDATE`, in.DocumentID, in.OwnerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return indexdoc.Empty{}, indexdoc.ErrNotFound
	}
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("authorize source access change: %w", err)
	}
	var existing indexdoc.AccessChange
	err = tx.QueryRow(ctx, `SELECT document_id,owner_id,revoked FROM source_access_events WHERE request_id=$1`,
		in.RequestID).Scan(&existing.DocumentID, &existing.OwnerID, &existing.Revoked)
	if err == nil {
		if existing.DocumentID != in.DocumentID || existing.OwnerID != in.OwnerID || existing.Revoked != in.Revoked {
			return indexdoc.Empty{}, indexdoc.ErrConflict
		}
		return indexdoc.Empty{}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return indexdoc.Empty{}, fmt.Errorf("read source access retry: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO source_access_events(request_id,document_id,owner_id,revoked) VALUES($1,$2,$3,$4)`,
		in.RequestID, in.DocumentID, in.OwnerID, in.Revoked)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("record source access: %w", err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO source_index_heads(document_id,revoked) VALUES($1,$2)
ON CONFLICT(document_id) DO UPDATE SET revoked=EXCLUDED.revoked,updated_at=clock_timestamp()
WHERE source_index_heads.revoked<>EXCLUDED.revoked`, in.DocumentID, in.Revoked)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("apply source access: %w", err)
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, `UPDATE source_corpus_epoch SET value=value+1 WHERE id=true`); err != nil {
			return indexdoc.Empty{}, fmt.Errorf("advance access epoch: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return indexdoc.Empty{}, fmt.Errorf("commit source access: %w", err)
	}
	return indexdoc.Empty{}, nil
}
