package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
)

type LLMUsageRepo struct{ q *sqlcgen.Queries }

func NewLLMUsageRepo(db *DB) *LLMUsageRepo { return &LLMUsageRepo{q: sqlcgen.New(db.Pool)} }

// SetDaily сохраняет монотонный итог независимо от порядка повторных записей.
func (r *LLMUsageRepo) SetDaily(ctx context.Context, row *domain.LLMUsageDaily) error {
	if row == nil || row.Requests < 0 || row.PromptTokens < 0 || row.CompletionTokens < 0 || row.CostNanoUSD < 0 {
		return domain.ErrInvalidArgument
	}
	err := r.q.SetLLMUsageDaily(ctx, sqlcgen.SetLLMUsageDailyParams{
		Day:              row.Day,
		Model:            row.Model,
		Operation:        row.Operation,
		Requests:         row.Requests,
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		CostNanoUsd:      row.CostNanoUSD,
	})
	if err != nil {
		return fmt.Errorf("set llm usage daily: %w", err)
	}
	return nil
}

func (r *LLMUsageRepo) ListDaily(ctx context.Context, from, to time.Time) ([]*domain.LLMUsageDaily, error) {
	rows, err := r.q.ListLLMUsageDaily(ctx, sqlcgen.ListLLMUsageDailyParams{FromDay: from, ToDay: to})
	if err != nil {
		return nil, fmt.Errorf("list llm usage daily: %w", err)
	}
	out := make([]*domain.LLMUsageDaily, 0, len(rows))
	for i := range rows {
		out = append(out, &domain.LLMUsageDaily{
			Day:              rows[i].Day,
			Model:            rows[i].Model,
			Operation:        rows[i].Operation,
			Requests:         rows[i].Requests,
			PromptTokens:     rows[i].PromptTokens,
			CompletionTokens: rows[i].CompletionTokens,
			CostNanoUSD:      rows[i].CostNanoUsd,
		})
	}
	return out, nil
}
