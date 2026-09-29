package postgres_test

import (
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/example/db-service/internal/platform/extractdoc"
)

func catalogue(t *testing.T, repo *postgres.TechnologyRepo, in extractdoc.SearchTechnologies,
) extractdoc.TechnologyPage {
	t.Helper()
	page, err := repo.SearchTechnologies(t.Context(), in)
	if err != nil {
		t.Fatalf("catalogue search: %v", err)
	}
	return page
}

func TestCatalogueFindsByMechanismAndAlias(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db).WithSharedCorpus(false)
	ctx := t.Context()
	q := sqlcgen.New(db.Pool)
	owner := uuid.NewV4().String()
	revision := seedPublicDoc(t, db, owner)
	seedExtraction(t, q, revision, "complete", 5)
	technology, _ := seedTechnology(t, db, revision, "Solid oxide stack "+uuid.NewV4().String()[:8])
	if err := q.AddTechnologyAlias(ctx, sqlcgen.AddTechnologyAliasParams{
		TechnologyID: technology, Value: "твердооксидный стек", Language: "ru", Basis: "expert_review",
	}); err != nil {
		t.Fatal(err)
	}

	// Совпадает механизм, а в названии нет ни одного слова запроса.
	byMechanism := catalogue(t, repo, extractdoc.SearchTechnologies{
		ReaderID: owner, Query: "маршрутизация по запросу", Limit: 20})
	if len(byMechanism.Technologies) == 0 {
		t.Fatalf("a query over the mechanism found nothing: %+v", byMechanism)
	}
	matched := false
	for _, hit := range byMechanism.Technologies {
		if hit.TechnologyID == technology {
			matched = true
			if !contains(hit.MatchReasons, extractdoc.MatchMechanism) {
				t.Fatalf("the reason for the match was not recorded: %+v", hit.MatchReasons)
			}
			if !strings.Contains(strings.ToLower(hit.MatchedText), "маршрутизация по запросу") {
				t.Fatalf("the matched mechanism text was not returned: %q", hit.MatchedText)
			}
		}
	}
	if !matched {
		t.Fatal("a query over the mechanism did not find the technology it describes")
	}

	byAlias := catalogue(t, repo, extractdoc.SearchTechnologies{
		ReaderID: owner, Query: "твердооксидный", Limit: 20})
	aliasMatched := false
	for _, hit := range byAlias.Technologies {
		if hit.TechnologyID == technology && contains(hit.MatchReasons, extractdoc.MatchAlias) {
			aliasMatched = true
			if hit.MatchedText != "твердооксидный стек" {
				t.Fatalf("the matched alias was not returned: %q", hit.MatchedText)
			}
		}
	}
	if !aliasMatched {
		t.Fatalf("a Russian query did not reach the English technology through its alias: %+v", byAlias)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCatalogueCursorPinsQueryAliasesAndAccess(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db).WithSharedCorpus(false)
	owner := uuid.NewV4().String()
	revision := seedPublicDoc(t, db, owner)
	q := sqlcgen.New(db.Pool)
	seedExtraction(t, q, revision, "complete", 5)
	technology, _ := seedTechnology(t, db, revision, "Первый результат")
	seedTechnology(t, db, revision, "Второй результат")
	in := extractdoc.SearchTechnologies{ReaderID: owner, Limit: 1, IncludeAll: true}
	first := catalogue(t, repo, in)
	if first.NextCursor == "" {
		t.Fatal("fixture needs two pages")
	}
	in.Cursor, in.Query = first.NextCursor, "результат"
	if _, err := repo.SearchTechnologies(t.Context(), in); !errors.Is(err, extractdoc.ErrConflict) {
		t.Fatalf("a cursor of another query was accepted: %v", err)
	}
	in.Query = ""
	if err := q.AddTechnologyAlias(t.Context(), sqlcgen.AddTechnologyAliasParams{
		TechnologyID: technology, Value: "new alias", Language: "en", Basis: "expert_review",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SearchTechnologies(t.Context(), in); !errors.Is(err, extractdoc.ErrConflict) {
		t.Fatalf("alias changed without invalidating cursor: %v", err)
	}
	in.Cursor = ""
	first = catalogue(t, repo, in)
	if _, err := db.Pool.Exec(t.Context(), `UPDATE documents
SET source_metadata=source_metadata || '{"access_scope":"shared"}'::jsonb
WHERE id=$1`,
		revision); err != nil {
		t.Fatal(err)
	}
	in.Cursor = first.NextCursor
	if _, err := repo.SearchTechnologies(t.Context(), in); !errors.Is(err, extractdoc.ErrConflict) {
		t.Fatalf("access changed without invalidating cursor: %v", err)
	}
	in.Cursor, in.ReaderID, in.SharedCorpus = "", uuid.NewV4().String(), true
	page := catalogue(t, repo, in)
	for _, hit := range page.Technologies {
		if hit.TechnologyID == technology {
			t.Fatal("request enabled sharing disabled by repository policy")
		}
	}
}

func TestCatalogueTreatsWildcardCharactersAsText(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db)
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	seedExtraction(t, sqlcgen.New(db.Pool), revision, "complete", 5)
	name := uuid.NewV4().String() + "_100%"
	technology, _ := seedTechnology(t, db, revision, name)
	seedTechnology(t, db, revision, name[:len(name)-5]+"x100x")
	page := catalogue(t, repo, extractdoc.SearchTechnologies{ReaderID: owner, Query: name, Limit: 20})
	if len(page.Technologies) != 1 || page.Technologies[0].TechnologyID != technology {
		t.Fatalf("SQL wildcards changed the literal query: %+v", page)
	}
}

func TestCatalogueRefusals(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewTechnologyRepo(db).WithSharedCorpus(false)
	ctx := t.Context()
	q := sqlcgen.New(db.Pool)
	owner, stranger := uuid.NewV4().String(), uuid.NewV4().String()
	private := seedDoc(t, db, owner)
	seedExtraction(t, q, private, "complete", 5)
	secret := "Закрытая технология " + uuid.NewV4().String()[:8]
	hidden, _ := seedTechnology(t, db, private, secret)

	empty := catalogue(t, repo, extractdoc.SearchTechnologies{ReaderID: owner, Limit: 20})
	if len(empty.Technologies) != 0 || empty.Reason != extractdoc.ReasonQueryRequired {
		t.Fatalf("an empty query returned the catalogue: %+v", empty)
	}
	asked := catalogue(t, repo, extractdoc.SearchTechnologies{ReaderID: owner, Limit: 20, IncludeAll: true})
	if len(asked.Technologies) == 0 {
		t.Fatal("an explicit request for the whole catalogue returned nothing")
	}
	// У страницы с ответом нет оправдания пустоты.
	if asked.Reason != "" {
		t.Fatalf("a page with results still gave a reason for being empty: %q", asked.Reason)
	}

	miss := catalogue(t, repo, extractdoc.SearchTechnologies{
		ReaderID: owner, Query: "нечто чего тут точно нет " + uuid.NewV4().String(), Limit: 20})
	if len(miss.Technologies) != 0 || miss.Reason != extractdoc.ReasonNoMatch {
		t.Fatalf("a miss was answered with the nearest thing found: %+v", miss)
	}

	byStranger := catalogue(t, repo, extractdoc.SearchTechnologies{
		ReaderID: stranger, Query: "закрытая технология", Limit: 20})
	for _, hit := range byStranger.Technologies {
		if hit.TechnologyID == hidden {
			t.Fatal("a private technology appeared in another account's search")
		}
	}

	if _, err := repo.SearchTechnologies(ctx, extractdoc.SearchTechnologies{
		ReaderID: owner, Query: "x", Limit: 20, Cursor: "0000000000000000000000000000000f:0",
	}); !errors.Is(err, extractdoc.ErrConflict) {
		t.Fatal("a cursor of another catalogue was accepted")
	}
	if _, err := repo.SearchTechnologies(ctx, extractdoc.SearchTechnologies{
		ReaderID: owner, Query: "x", Limit: 20, Stages: []string{"emerging_signal"},
	}); !errors.Is(err, extractdoc.ErrInvalid) {
		t.Fatal("a filter that cannot be applied was accepted and ignored")
	}
}
