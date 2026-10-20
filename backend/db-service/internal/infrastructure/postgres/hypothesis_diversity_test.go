package postgres_test

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestHypothesisDiversityPrecedesPagination(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	_, err := db.Pool.Exec(ctx, `INSERT INTO hypotheses(id,owner_id,title,statement,function_area,composite_score)
 SELECT $1||'-'||n,$1,'Forecast '||n,'Study',
 CASE WHEN n=206 THEN 'Медицина' ELSE 'Генеративное ранжирование '||n END,
 CASE WHEN n=206 THEN 0.1 ELSE 0.9 END FROM generate_series(1,206) n`, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `DELETE FROM hypotheses WHERE owner_id=$1`, owner) })
	repo := postgres.NewHypothesisRepo(db)
	filter := domain.HypothesisFilter{OwnerID: owner, OrderBy: "diversity", Limit: 2}
	first, _, err := repo.Board(ctx, filter)
	if err != nil || len(first) != 2 || !strings.HasPrefix(first[0].FunctionArea, "Генеративное") ||
		first[1].FunctionArea != "Медицина" {
		t.Fatalf("small domain was lost before pagination: %+v %v", first, err)
	}
	filter.Offset = 2
	next, _, err := repo.Board(ctx, filter)
	if err != nil || len(next) != 2 || next[0].ID == first[0].ID || next[1].ID == first[1].ID {
		t.Fatalf("unstable pagination: %+v %v", next, err)
	}
	filter.Offset, filter.OrderBy = 0, ""
	ranked, _, err := repo.Board(ctx, filter)
	if err != nil || len(ranked) != 2 || !strings.HasPrefix(ranked[0].FunctionArea, "Генеративное") ||
		!strings.HasPrefix(ranked[1].FunctionArea, "Генеративное") {
		t.Fatalf("explicit priority changed: %+v %v", ranked, err)
	}
	filter.OrderBy, filter.FunctionArea = "diversity", "Медицина"
	medical, _, err := repo.Board(ctx, filter)
	if err != nil || len(medical) != 1 || medical[0].FunctionArea != "Медицина" {
		t.Fatalf("explicit domain filter changed: %+v %v", medical, err)
	}
	filter.OwnerID = uuid.NewV4().String()
	foreign, _, err := repo.Board(ctx, filter)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("private forecasts leaked: %+v %v", foreign, err)
	}
}
