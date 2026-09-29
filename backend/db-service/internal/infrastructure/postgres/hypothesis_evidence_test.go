package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

// Повторная проверка должна заменить прежнюю поддержку контекстом и убрать выбывший фрагмент
// в самой базе, а не только в ответе сервиса. Чужой идентификатор откатывает весь результат.
func TestVerifiedEvidenceReplacesPersistedStancesAtomically(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewHypothesisRepo(db)
	ctx := t.Context()
	owner, foreignOwner := uuid.NewV4().String(), uuid.NewV4().String()
	t.Cleanup(func() {
		if _, err := db.Pool.Exec(context.Background(), `DELETE FROM hypotheses WHERE owner_id=ANY($1::text[])`,
			[]string{owner, foreignOwner}); err != nil {
			t.Error(err)
		}
	})
	makeHypothesis := func(ownerID string) *domain.Hypothesis {
		h := &domain.Hypothesis{
			ID: uuid.NewV4().String(), OwnerID: ownerID, Title: "Бенчмарк", Statement: "Внедрение повысит точность",
			Status: domain.HypothesisGenerated, Method: "manual", ConfidenceScore: new(0.95),
			Assessment: []byte(`{"check":{"verdict":"supported"}}`), CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		for i, stance := range []string{"supports", "contradicts"} {
			h.Evidence = append(h.Evidence, &domain.Evidence{
				ID: uuid.NewV4().String(), HypothesisID: h.ID, ChunkID: uuid.NewV4().String(), Filename: "benchmark.pdf",
				Snippet: "Macro-F1 drops on harder negatives.", Stance: stance, Relation: "Старое толкование",
				Ord: i, CreatedAt: time.Now(),
			})
		}
		if err := repo.Create(ctx, h, nil); err != nil {
			t.Fatal(err)
		}
		return h
	}
	h, foreign := makeHypothesis(owner), makeHypothesis(foreignOwner)
	retainedID, removedID := h.Evidence[0].ID, h.Evidence[1].ID
	h.Evidence = h.Evidence[:1]
	h.Evidence[0].Stance, h.Evidence[0].Relation = "context", ""
	h.Evidence = append(h.Evidence, &domain.Evidence{
		ChunkID: "new-fragment", Filename: "new.pdf", Snippet: "No deployment experiment.", Stance: "context", Ord: 2,
	})
	h.ConfidenceScore = new(0.0)
	h.Assessment = []byte(`{"check":{"verdict":"insufficient"}}`)
	revision := func(action string) *domain.Revision {
		return &domain.Revision{ID: uuid.NewV4().String(), HypothesisID: h.ID, Action: action, CreatedAt: time.Now()}
	}
	if err := repo.Update(ctx, h, revision("verified")); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Get(ctx, h.ID)
	if err != nil || len(saved.Evidence) != 2 || saved.ConfidenceScore == nil || *saved.ConfidenceScore != 0 {
		t.Fatalf("persisted verification: %+v, %v", saved, err)
	}
	for _, e := range saved.Evidence {
		if e.ID == "" || e.ID == removedID || e.Stance != "context" || e.Relation != "" {
			t.Fatalf("old support or removed evidence survived verification: %+v", e)
		}
	}
	if saved.Evidence[0].ID != retainedID || saved.Evidence[1].ChunkID != "new-fragment" {
		t.Fatalf("retained and new evidence were not saved: %+v", saved.Evidence)
	}

	bad := *saved
	bad.Title, bad.ConfidenceScore = "Не должно сохраниться", new(0.99)
	foreignEvidence := *foreign.Evidence[0]
	foreignEvidence.HypothesisID, foreignEvidence.Stance = h.ID, "context"
	bad.Evidence = []*domain.Evidence{&foreignEvidence}
	if err = repo.Update(ctx, &bad, revision("verified")); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("foreign evidence ID accepted: %v", err)
	}
	afterRejected, err := repo.Get(ctx, h.ID)
	if err != nil || afterRejected.Version != saved.Version || afterRejected.Title != saved.Title ||
		afterRejected.ConfidenceScore == nil || *afterRejected.ConfidenceScore != 0 || len(afterRejected.Evidence) != 2 {
		t.Fatalf("invalid verification did not roll back: %+v, %v", afterRejected, err)
	}
	other, err := repo.Get(ctx, foreign.ID)
	if err != nil || other.Evidence[0].Stance != "supports" {
		t.Fatalf("foreign hypothesis changed: %+v, %v", other, err)
	}

	// Обычная правка карточки без списка свидетельств не должна удалять их.
	saved.Evidence = nil
	if err = repo.Update(ctx, saved, nil); err != nil {
		t.Fatal(err)
	}
	ordinary, err := repo.Get(ctx, h.ID)
	if err != nil || len(ordinary.Evidence) != 2 {
		t.Fatalf("ordinary update removed evidence: %+v, %v", ordinary, err)
	}
	ordinary.Evidence = nil
	if err = repo.Update(ctx, ordinary, revision("verified")); err != nil {
		t.Fatal(err)
	}
	empty, err := repo.Get(ctx, h.ID)
	if err != nil || len(empty.Evidence) != 0 {
		t.Fatalf("empty verified set retained old evidence: %+v, %v", empty, err)
	}
	revisions, err := repo.ListRevisions(ctx, h.ID)
	if err != nil || len(revisions) != 2 || revisions[0].RevisionNo != 1 || revisions[1].RevisionNo != 2 {
		t.Fatalf("failed verification changed revision history: %+v, %v", revisions, err)
	}
}
