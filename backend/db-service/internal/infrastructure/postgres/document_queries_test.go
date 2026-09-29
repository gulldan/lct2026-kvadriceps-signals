package postgres_test

import (
	"context"
	"encoding/json"
	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/contracts"
	"testing"
	"time"
	"uuid"
)

func TestCatalogPagesAndAggregates(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	_, err := db.Pool.Exec(ctx, `INSERT INTO documents(id,owner_id,filename,mime_type,size,object_key,status,created_at,updated_at)
 SELECT $1||'-'||lpad(n::text,4,'0'),$1,'literal%_'||n||'.pdf','application/pdf',10,'page/'||$1||n,'indexed',now(),now()
 FROM generate_series(1,601) n`, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `DELETE FROM documents WHERE owner_id=$1`, owner) })
	rawRepo := postgres.NewDocumentRepo(db)
	firstBatch, err := rawRepo.ListByOwner(ctx, owner, contracts.DocumentScan{})
	if err != nil || len(firstBatch) != 500 {
		t.Fatalf("raw scan first: %d %v", len(firstBatch), err)
	}
	lastBatch, err := rawRepo.ListByOwner(ctx, owner, contracts.DocumentScan{BeforeID: firstBatch[499].ID})
	if err != nil || len(lastBatch) != 101 {
		t.Fatalf("raw scan tail: %d %v", len(lastBatch), err)
	}
	repo := postgres.NewDocumentQueries(db, true)
	query := contracts.DocumentQuery{ReaderID: owner, Query: "literal%_", Limit: 100}
	first, err := repo.Page(ctx, query)
	if err != nil || first.Total != 601 || len(first.Documents) != 100 || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	ids := map[string]bool{}
	for _, d := range first.Documents {
		if d.ObjectKey != "" {
			t.Fatal("storage key leaked in list")
		}
		ids[d.ID] = true
	}
	query.BeforeID = first.NextCursor
	second, err := repo.Page(ctx, query)
	if err != nil || second.Total != 601 || len(second.Documents) != 100 {
		t.Fatalf("second page: %+v %v", second, err)
	}
	for _, d := range second.Documents {
		if ids[d.ID] {
			t.Fatal("unstable tie ordering")
		}
	}
	query.Offset = 9999
	empty, err := repo.Page(ctx, query)
	if err != nil || empty.Total != 601 || len(empty.Documents) != 0 {
		t.Fatalf("empty page: %+v %v", empty, err)
	}
	query.Offset = 0
	query.BeforeID = ""
	stats, err := repo.Stats(ctx, query)
	if err != nil ||
		stats.Total != 601 ||
		stats.TotalSizeBytes != 6010 ||
		stats.ByFileType["pdf"] != 601 ||
		stats.IndexedPct != 100 ||
		len(
			stats.UploadsByDay,
		) != 14 ||
		len(
			stats.TopByChunks,
		) != 10 {
		t.Fatalf("SQL stats: %+v %v", stats, err)
	}
	query.ReaderID = uuid.NewV4().String()
	invisible, err := repo.Page(ctx, query)
	if err != nil || invisible.Total != 0 {
		t.Fatalf("foreign owner: %+v %v", invisible, err)
	}
	query.AllOwners = true
	if _, err = repo.Page(ctx, query); err == nil {
		t.Fatal("unprivileged all owners accepted")
	}

	hypotheses := postgres.NewHypothesisRepo(db)
	for range 105 {
		h := &domain.Hypothesis{
			ID:        uuid.NewV4().String(),
			OwnerID:   owner,
			RunID:     "pages",
			Title:     "needle",
			Statement: "test",
			Method:    "manual",
			Status:    "generated",
			Assessment: []byte(
				`{"check":{"verdict":"supported","large":"omit"}}`,
			),
			Detail: []byte(
				`{"private":"omit"}`,
			),
			Generation: []byte(
				`{"model":"x","response":{"large":"omit"}}`,
			),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err = hypotheses.Create(ctx, h, nil); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `DELETE FROM hypotheses WHERE owner_id=$1`, owner) })
	hs, raw, err := hypotheses.Board(
		ctx,
		domain.HypothesisFilter{
			OwnerID: owner,
			Search:  "needle",
			Queue:   "ready",
			Limit:   10,
			Offset:  100,
			Compact: true,
		},
	)
	var aggregate struct {
		Total  int            `json:"total"`
		Queues map[string]int `json:"queue_counts"`
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &aggregate); err != nil {
		t.Fatal(err)
	}
	if len(hs) != 5 || aggregate.Total != 105 || aggregate.Queues["ready"] != 105 {
		t.Fatalf("board: %d %+v", len(hs), aggregate)
	}
	if string(hs[0].Detail) != "{}" || string(hs[0].Generation) != `{"model": "x"}` {
		t.Fatalf("noncompact list: %+v", hs[0])
	}
}
