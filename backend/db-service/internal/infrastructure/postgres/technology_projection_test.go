package postgres_test

import (
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/example/db-service/internal/platform/extractdoc"
)

func TestPrivateRevisionDoesNotMovePublicNumbers(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db).WithSharedCorpus(false)
	ctx := t.Context()
	q := sqlcgen.New(db.Pool)
	author, stranger := uuid.NewV4().String(), uuid.NewV4().String()

	open := seedPublicDoc(t, db, author)
	closed := seedDoc(t, db, author)
	for _, revision := range []string{open, closed} {
		seedExtraction(t, q, revision, "complete", 5)
	}
	technology, _ := seedTechnology(t, db, open, "Мембранный электролизёр")
	attachMention(t, db, technology, closed, "Мембранный электролизёр")
	attachMention(t, db, technology, open, "Второе упоминание в той же ревизии")

	owner, err := repo.ReadTechnology(ctx, extractdoc.ReadTechnology{ReaderID: author, TechnologyID: technology})
	if err != nil {
		t.Fatal(err)
	}
	if owner.ReadableRevisions != 2 || owner.RestrictedRevisions != 0 || len(owner.Mentions) != 3 {
		t.Fatalf("the author does not see their own two revisions: %+v", owner)
	}

	other, err := repo.ReadTechnology(ctx, extractdoc.ReadTechnology{ReaderID: stranger, TechnologyID: technology})
	if err != nil {
		t.Fatal(err)
	}
	if other.ReadableRevisions != 1 || other.RestrictedRevisions != 1 || len(other.Mentions) != 2 {
		t.Fatalf("the private revision leaked into another account's projection: %+v", other)
	}
	// Скрытые упоминания считаются, но не называются.
	for _, mention := range other.Mentions {
		for _, quote := range mention.Evidence {
			if quote.Quote == "" {
				continue
			}
			if quote.EvidenceID == "" {
				t.Fatal("a readable quote lost its identity")
			}
		}
	}
	if other.CanonicalName != owner.CanonicalName {
		t.Fatal("the two projections disagree about the technology itself")
	}
}
