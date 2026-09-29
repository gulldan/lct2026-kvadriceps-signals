package postgres

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/jackc/pgx/v5"
)

// Read перепроверяет каждый источник; один отзыв скрывает всё представление.
func (r *AnalysisRepo) Read(ctx context.Context, in analysisdoc.Read) (analysisdoc.Frozen, error) {
	_, runErr := uuid.Parse(in.RunID)
	_, readerErr := uuid.Parse(in.ReaderID)
	if runErr != nil || readerErr != nil {
		return analysisdoc.Frozen{}, analysisdoc.ErrInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("begin analysis read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var frozen analysisdoc.Frozen
	err = tx.QueryRow(ctx, `SELECT run_id,manifest,manifest_hash FROM signal_analysis_inputs
WHERE run_id=$1 AND owner_id=$2`, in.RunID, in.ReaderID).Scan(&frozen.RunID, &frozen.JSON, &frozen.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return frozen, analysisdoc.ErrNotFound
	}
	if err != nil {
		return frozen, fmt.Errorf("read frozen analysis: %w", err)
	}
	if err = checkAnalysisAccess(ctx, tx, in.RunID, in.ReaderID, in.SharedCorpus); err != nil {
		return analysisdoc.Frozen{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("commit analysis read: %w", err)
	}
	return finishFrozen(frozen)
}

func checkAnalysisAccess(ctx context.Context, tx pgx.Tx, runID, reader string, shared bool) error {
	var denied bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM signal_analysis_sources a LEFT JOIN documents d ON d.id=a.document_id
LEFT JOIN source_index_heads h ON h.document_id=d.id
LEFT JOIN source_index_snapshots s ON s.document_id=d.id AND s.snapshot_id=a.snapshot_id AND s.ready
WHERE a.run_id=$1 AND (d.id IS NULL OR (a.snapshot_id<>'' AND s.snapshot_id IS NULL) OR d.source_metadata IS NULL
OR coalesce(h.revoked,false) OR NOT (d.owner_id=$2 OR d.source_metadata->>'access_scope'='public'
OR ($3 AND d.source_metadata->>'access_scope'='shared'))))`, runID, reader, shared).Scan(&denied)
	if err != nil {
		return fmt.Errorf("check frozen analysis access: %w", err)
	}
	if !denied {
		return nil
	}
	// Владелец и так знает, что анализ существует, поэтому ему говорят, что изменилось,
	// а не отдают старый результат или голое «недоступно».
	var owned bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM signal_analysis_inputs
WHERE run_id=$1 AND owner_id=$2)`, runID, reader).Scan(&owned); err != nil {
		return fmt.Errorf("check analysis ownership: %w", err)
	}
	if owned {
		return analysisdoc.ErrAccessChanged
	}
	return analysisdoc.ErrNotFound
}
