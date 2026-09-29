package postgres_test

import (
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestHypothesisVersionFencesStaleWritersAndSerializesRevisions(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewHypothesisRepo(db)
	h := &domain.Hypothesis{ID: uuid.NewV4().String(), OwnerID: uuid.NewV4().String(),
		Title: "Исходная гипотеза", Statement: "Утверждение", Status: "generated", Method: "manual",
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Create(t.Context(), h, nil); err != nil {
		t.Fatal(err)
	}
	stale := *h
	h.Status = "approved"
	rev := &domain.Revision{ID: uuid.NewV4().String(), HypothesisID: h.ID, Action: "approved", CreatedAt: time.Now()}
	if err := repo.Update(t.Context(), h, rev); err != nil {
		t.Fatal(err)
	}
	stale.Statement = "Запоздавший ответ модели"
	if err := repo.Update(t.Context(), &stale, nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale writer accepted: %v", err)
	}
	got, err := repo.Get(t.Context(), h.ID)
	if err != nil || got.Version != 2 || got.Status != "approved" || got.Statement != h.Statement {
		t.Fatalf("approved state lost: %+v %v", got, err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			revision := &domain.Revision{ID: uuid.NewV4().String(), HypothesisID: h.ID,
				Action: "commented", CreatedAt: time.Now()}
			if stepErr := repo.AddRevision(t.Context(), revision); stepErr != nil {
				t.Errorf("concurrent revision: %v", stepErr)
			}
		})
	}
	wg.Wait()
	revisions, err := repo.ListRevisions(t.Context(), h.ID)
	if err != nil || len(revisions) != 17 {
		t.Fatalf("revision sequence: %d %v", len(revisions), err)
	}
	for i, revision := range revisions {
		if revision.RevisionNo != i+1 {
			t.Fatalf("noncontiguous revision: %+v", revision)
		}
	}
}
