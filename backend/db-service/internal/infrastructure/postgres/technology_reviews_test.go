package postgres_test

import (
	"errors"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/example/db-service/internal/platform/extractdoc"
)

func review(technology, author, operation string) extractdoc.Review {
	return extractdoc.Review{ID: uuid.NewV4().String(), TechnologyID: technology, AuthorID: author,
		Operation: operation, Reason: "эксперт исправил название по формуле патента",
		PreviousValue: map[string]any{"canonical_name": "Мембранный электролизёр"},
		NewValue:      map[string]any{"canonical_name": "Электролизёр на протонообменной мембране"}}
}

func TestMergeAndRevertKeepTheirHistory(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, sqlcgen.New(db.Pool), revision, "complete", 3)
	first, _ := seedTechnology(t, db, revision, "Мембранный электролизёр")
	second, _ := seedTechnology(t, db, revision, "Электролизёр на мембране")

	merge := review(first, owner, extractdoc.OpMerge)
	merge.NewValue = map[string]any{"same_as": second}
	merge.PreviousValue = map[string]any{"same_as": nil}
	merged, err := repo.Review(ctx, merge)
	if err != nil || merged.Operation != extractdoc.OpMerge {
		t.Fatalf("merge: %+v %v", merged, err)
	}
	for _, id := range []string{first, second} {
		if _, err = sqlcgen.New(db.Pool).GetTechnology(ctx, id); err != nil {
			t.Fatalf("a merged technology lost its identifier %s: %v", id, err)
		}
	}

	undo := review(first, owner, extractdoc.OpRevert)
	undo.RevertsID = merged.ID
	undo.PreviousValue, undo.NewValue = merge.NewValue, merge.PreviousValue
	reverted, err := repo.Review(ctx, undo)
	if err != nil || reverted.RevertsID != merged.ID {
		t.Fatalf("revert: %+v %v", reverted, err)
	}
	var linked bool
	if err = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM technology_relations
WHERE relation='same_as' AND technology_id=ANY($1::text[]) AND related_technology_id=ANY($1::text[]))`,
		[]string{first, second}).Scan(&linked); err != nil || linked {
		t.Fatalf("revert left the merge active: linked=%v err=%v", linked, err)
	}
	history, err := repo.Reviews(ctx, extractdoc.ReadReviews{ReaderID: owner, TechnologyID: first})
	if err != nil || len(history) != 2 {
		t.Fatalf("the history does not hold both operations: %d %v", len(history), err)
	}
	if history[0].Operation != extractdoc.OpRevert || history[1].Operation != extractdoc.OpMerge {
		t.Fatalf("the history is not in order: %+v", history)
	}
	if history[1].PreviousValue == nil {
		t.Fatal("a correction was stored without the value it replaced, so it cannot be undone")
	}
}

func TestReviewReplayDoesNotOverwriteANewerCorrection(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, sqlcgen.New(db.Pool), revision, "complete", 3)
	technology, _ := seedTechnology(t, db, revision, "Мембранный электролизёр")
	first := review(technology, owner, extractdoc.OpRename)
	if _, err := repo.Review(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	next := review(technology, owner, extractdoc.OpRename)
	next.PreviousValue = first.NewValue
	next.NewValue = map[string]any{"canonical_name": "Новая проверенная формулировка"}
	stored, err := repo.Review(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Review(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	got, err := sqlcgen.New(db.Pool).GetTechnology(t.Context(), technology)
	if err != nil || got.CanonicalName != next.NewValue["canonical_name"] {
		t.Fatalf("retry of an older correction overwrote the current value: %+v %v", got, err)
	}
	undo := review(technology, owner, extractdoc.OpRevert)
	undo.RevertsID = stored.ID
	if _, err = repo.Review(t.Context(), undo); err != nil {
		t.Fatal(err)
	}
	got, err = sqlcgen.New(db.Pool).GetTechnology(t.Context(), technology)
	if err != nil || got.CanonicalName != first.NewValue["canonical_name"] {
		t.Fatalf("revert did not restore the saved previous value: %+v %v", got, err)
	}
}

func TestRepeatedReviewIsOneVersion(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, sqlcgen.New(db.Pool), revision, "complete", 3)
	technology, _ := seedTechnology(t, db, revision, "Мембранный электролизёр")

	first, err := repo.Review(ctx, review(technology, owner, extractdoc.OpRename))
	if err != nil {
		t.Fatal(err)
	}
	again := review(technology, owner, extractdoc.OpRename)
	second, err := repo.Review(ctx, again)
	if err != nil || second.ID != first.ID {
		t.Fatalf("the same correction was recorded twice: %s then %s (%v)", first.ID, second.ID, err)
	}
	history, err := repo.Reviews(ctx, extractdoc.ReadReviews{ReaderID: owner, TechnologyID: technology})
	if err != nil || len(history) != 1 {
		t.Fatalf("the history holds %d versions of one decision: %v", len(history), err)
	}
	stored, err := sqlcgen.New(db.Pool).GetTechnology(ctx, technology)
	if err != nil || stored.CanonicalName != "Электролизёр на протонообменной мембране" {
		t.Fatalf("the correction was not applied: %+v %v", stored, err)
	}
}

func TestReviewRefusals(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, sqlcgen.New(db.Pool), revision, "complete", 3)
	technology, _ := seedTechnology(t, db, revision, "Мембранный электролизёр")

	for name, broken := range map[string]func(*extractdoc.Review){
		"no reason":              func(r *extractdoc.Review) { r.Reason = "   " },
		"unknown operation":      func(r *extractdoc.Review) { r.Operation = "improve" },
		"revert naming nothing":  func(r *extractdoc.Review) { r.Operation = extractdoc.OpRevert },
		"a target without a fix": func(r *extractdoc.Review) { r.RevertsID = uuid.NewV4().String() },
		"no previous value":      func(r *extractdoc.Review) { r.PreviousValue = nil },
	} {
		in := review(technology, owner, extractdoc.OpRename)
		broken(&in)
		if _, err := repo.Review(ctx, in); !errors.Is(err, extractdoc.ErrInvalid) {
			t.Fatalf("a correction that cannot be explained or undone was recorded: %s (%v)", name, err)
		}
	}
	stranger := review(technology, uuid.NewV4().String(), extractdoc.OpRename)
	if _, err := repo.Review(ctx, stranger); !errors.Is(err, extractdoc.ErrNotFound) {
		t.Fatalf("another account corrected a technology it cannot read: %v", err)
	}
	if _, err := repo.Reviews(ctx, extractdoc.ReadReviews{
		ReaderID: uuid.NewV4().String(), TechnologyID: technology,
	}); !errors.Is(err, extractdoc.ErrNotFound) {
		t.Fatal("another account read the correction history")
	}
}
