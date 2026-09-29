package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/jackc/pgx/v5/pgconn"
)

// SetSource пишется один раз, только владельцем и идемпотентен для тех же метаданных.
func (r *DocumentRepo) SetSource(ctx context.Context, id, ownerID string, metadata []byte) error {
	n, err := r.q.SetDocumentSource(ctx, sqlcgen.SetDocumentSourceParams{ID: id, OwnerID: ownerID, Metadata: metadata})
	if pgerr, ok := errors.AsType[*pgconn.PgError](err); ok && pgerr.Code == "23505" {
		return fmt.Errorf("source revision already belongs to a document: %w", domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("set document source: %w", err)
	}
	if n == 1 {
		return nil
	}
	d, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.OwnerID != ownerID {
		return domain.ErrNotFound
	}
	return fmt.Errorf("source revision is immutable: %w", domain.ErrConflict)
}
