package postgres_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestUsageMirrorCannotRegress(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewLLMUsageRepo(db)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	row := &domain.LLMUsageDaily{Day: day, Model: uuid.NewV4().String(), Operation: "answer",
		Requests: 20, PromptTokens: 100, CompletionTokens: 50, CostNanoUSD: 10}
	if err := repo.SetDaily(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	row.Requests, row.PromptTokens, row.CompletionTokens, row.CostNanoUSD = 2, 10, 5, 1
	if err := repo.SetDaily(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListDaily(t.Context(), day, day)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range rows {
		if got.Model == row.Model {
			if got.Requests != 20 || got.PromptTokens != 100 || got.CompletionTokens != 50 || got.CostNanoUSD != 10 {
				t.Fatalf("regressed total: %+v", got)
			}
			return
		}
	}
	t.Fatal("missing ledger row")
}
