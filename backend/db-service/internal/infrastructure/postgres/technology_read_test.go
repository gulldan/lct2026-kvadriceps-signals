package postgres_test

import (
	"errors"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/example/db-service/internal/platform/extractdoc"
)

func TestRevisionTechnologiesAreReadBackWhole(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	ctx := t.Context()
	q := sqlcgen.New(db.Pool)
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, q, revision, "complete", 12)
	for _, name := range []string{"Мембранный электролизёр", "Перовскитный модуль", "Натриевый аккумулятор"} {
		seedTechnology(t, db, revision, name)
	}

	out, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{ReaderID: owner, SourceRevisionID: revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Technologies) != 3 || out.Coverage.Status != "complete" || out.Coverage.TotalSections != 12 {
		t.Fatalf("a revision with three technologies read back as %+v", out)
	}
	for _, mention := range out.Technologies {
		if mention.Role != "proposed_contribution" || len(mention.Evidence) != 1 ||
			mention.Evidence[0].Quote == "" || mention.Evidence[0].Locator.Section != "method" {
			t.Fatalf("a mention lost its role or its quote: %+v", mention)
		}
		if mention.Evidence[0].Locator.ElementPath == "" || mention.Evidence[0].Locator.SnapshotSHA256 == "" {
			t.Fatalf("a quote lost part of its locator: %+v", mention.Evidence[0].Locator)
		}
		if len(mention.FieldEvidence["mechanism"]) != 1 {
			t.Fatalf("a field lost the quote that supports it: %+v", mention.FieldEvidence)
		}
	}

	// Три пустых ответа — разные ответы.
	read := seedDoc(t, db, owner)
	seedExtraction(t, q, read, "no_technology", 7)
	empty, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{ReaderID: owner, SourceRevisionID: read})
	if err != nil || len(empty.Technologies) != 0 || empty.Coverage.Status != "no_technology" ||
		empty.Coverage.ProcessedSections != 7 {
		t.Fatalf("a fully read revision holding nothing: %+v %v", empty, err)
	}
	broken := seedDoc(t, db, owner)
	seedExtraction(t, q, broken, "failed", 0)
	failed, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{ReaderID: owner, SourceRevisionID: broken})
	if err != nil || failed.Coverage.Status != "failed" {
		t.Fatalf("a broken attempt was not told apart from an empty one: %+v %v", failed, err)
	}
	untouched := seedDoc(t, db, owner)
	fresh, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{ReaderID: owner, SourceRevisionID: untouched})
	if err != nil || fresh.Coverage.Status != "not_extracted" || len(fresh.Technologies) != 0 {
		t.Fatalf("a revision nobody examined looked like one holding nothing: %+v %v", fresh, err)
	}
}

func TestRevisionTechnologiesRespectAccess(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, sqlcgen.New(db.Pool), revision, "complete", 3)
	seedTechnology(t, db, revision, "Мембранный электролизёр")

	if _, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{
		ReaderID: uuid.NewV4().String(), SourceRevisionID: revision,
	}); !errors.Is(err, extractdoc.ErrNotFound) {
		t.Fatalf("another account read a private revision: %v", err)
	}
	if _, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{
		ReaderID: owner, SourceRevisionID: uuid.NewV4().String(),
	}); !errors.Is(err, extractdoc.ErrNotFound) {
		t.Fatalf("a revision that does not exist answered differently: %v", err)
	}
	for name, in := range map[string]extractdoc.ReadRevision{
		"no reader":   {SourceRevisionID: revision},
		"no revision": {ReaderID: owner},
	} {
		if _, err := repo.ReadRevision(ctx, in); !errors.Is(err, extractdoc.ErrInvalid) {
			t.Fatalf("an incomplete read was accepted: %s", name)
		}
	}
}

func TestOlderExtractionIsReadBackAsItself(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	ctx := t.Context()
	q := sqlcgen.New(db.Pool)
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, q, revision, "partial", 4)
	if err := q.RecordTechnologyExtraction(ctx, sqlcgen.RecordTechnologyExtractionParams{
		SourceRevisionID: revision, ExtractorVersion: "extractor-test-v2", SchemaVersion: schemaVersion,
		Status: "complete", ProcessedSections: 9, TotalSections: 9,
	}); err != nil {
		t.Fatal(err)
	}
	newest, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{ReaderID: owner, SourceRevisionID: revision})
	if err != nil || newest.ExtractorVersion != "extractor-test-v2" || newest.Coverage.Status != "complete" {
		t.Fatalf("the newest extraction did not answer by default: %+v %v", newest, err)
	}
	older, err := repo.ReadRevision(ctx, extractdoc.ReadRevision{ReaderID: owner,
		SourceRevisionID: revision, ExtractorVersion: extractorVersion})
	if err != nil || older.ExtractorVersion != extractorVersion || older.Coverage.Status != "partial" ||
		older.Coverage.TotalSections != 4 {
		t.Fatalf("a named extraction answered with another one: %+v %v", older, err)
	}
}
