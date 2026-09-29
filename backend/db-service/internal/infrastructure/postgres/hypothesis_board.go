package postgres

import "fmt"

import (
	"context"
	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

// Board читает страницу и агрегаты из одного снимка, без полного корпуса в памяти.
func (r *HypothesisRepo) Board(ctx context.Context, f domain.HypothesisFilter) ([]*domain.Hypothesis, []byte, error) {
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, fmt.Errorf("board: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	rows, err := q.ListHypotheses(ctx, hypothesisListParams(f))
	if err != nil {
		return nil, nil, err
	}
	stats, err := q.HypothesisBoardStats(ctx, sqlcgen.HypothesisBoardStatsParams{
		OwnerID: f.OwnerID, Status: f.Status, KpiID: f.KPIID, ClusterID: f.ClusterID, FunctionArea: f.FunctionArea,
		SourceType: f.SourceType, Organization: f.Organization, MinTrl: int32(f.MinTRL), MaxTrl: int32(f.MaxTRL),
		Tags: orEmpty(f.Tags), DocumentIds: orEmpty(f.DocumentIDs), Search: f.Search, Queue: f.Queue})
	if err != nil {
		return nil, nil, err
	}
	out := make([]*domain.Hypothesis, 0, len(rows))
	for _, row := range rows {
		out = append(out, hypothesisFromRow(sqlcgen.Hypothesis(row)))
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return out, stats, fmt.Errorf("board: %w", operationErr)
	}
	return out, stats, nil
}
