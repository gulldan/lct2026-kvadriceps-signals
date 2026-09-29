package postgres_test

import (
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/harvest"
)

func collectionRequest(owner string) harvest.Request {
	return harvest.Request{OwnerID: owner, RequestID: uuid.NewV4().String(), Provider: "crossref",
		Scope: harvest.Scope{Query: "protonoobmennaya membrana " + uuid.NewV4().String(),
			From: "2025-01-01", Through: "2026-01-01", PageSize: 50, WorkType: "journal-article"},
		Limit: 100}
}

func TestCollectionRequestIsIdempotentOnRequestID(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewHarvestRepo(db).WithPanel([]string{"news.mit.edu", "nasa.gov"})
	ctx := t.Context()
	owner := uuid.NewV4().String()
	in := collectionRequest(owner)

	first, err := repo.Request(ctx, in)
	if err != nil || first.State != harvest.StateRequested || first.RequestID != in.RequestID {
		t.Fatalf("a requested collection was not recorded: %+v %v", first, err)
	}
	again, err := repo.Request(ctx, in)
	if err != nil || again.ScopeID != first.ScopeID || again.RequestedAt != first.RequestedAt {
		t.Fatalf("a repeated request created a second collection: %+v %v", again, err)
	}
	changed := in
	changed.Limit = 50
	if _, err = repo.Request(ctx, changed); !errors.Is(err, harvest.ErrConflict) {
		t.Fatalf("the same request_id with another input was accepted: %v", err)
	}

	progress, err := repo.Progress(ctx, harvest.Progress{OwnerID: owner, ScopeID: first.ScopeID})
	if err != nil || progress.ScopeID != first.ScopeID || progress.Manifests == nil {
		t.Fatalf("progress: %+v %v", progress, err)
	}
	// «Не найден», а не «пусто»: пустой ответ подтвердил бы, что сбор существует.
	if _, err = repo.Progress(ctx, harvest.Progress{OwnerID: uuid.NewV4().String(),
		ScopeID: first.ScopeID}); !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("another account read the collection: %v", err)
	}

	waiting, err := repo.Requested(ctx, harvest.Requested{Provider: "crossref", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, collection := range waiting {
		found = found || collection.ScopeID == first.ScopeID
	}
	if !found {
		t.Fatal("the collector was not offered the collection a user asked for")
	}
}

func TestCollectionPerimeterIsCheckedBeforePosting(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewHarvestRepo(db).WithPanel([]string{"news.mit.edu"})
	ctx := t.Context()
	owner := uuid.NewV4().String()

	outside := collectionRequest(owner)
	outside.Provider, outside.URLs = "web", []string{"https://evil.example/story"}
	if _, err := repo.Request(ctx, outside); !errors.Is(err, harvest.ErrInvalid) {
		t.Fatalf("an address outside the configured domains was posted: %v", err)
	}
	inside := collectionRequest(owner)
	inside.Provider, inside.URLs = "web", []string{"https://news.mit.edu/2026/story"}
	if _, err := repo.Request(ctx, inside); err != nil {
		t.Fatalf("a configured address was refused: %v", err)
	}
	for name, broken := range map[string]func(*harvest.Request){
		"url list without the web provider": func(r *harvest.Request) { r.URLs = []string{"https://news.mit.edu/a"} },
		"web provider without a url list":   func(r *harvest.Request) { r.Provider = "web" },
		"plain http":                        func(r *harvest.Request) { r.Provider, r.URLs = "web", []string{"http://news.mit.edu/a"} },
		"credentials in the address": func(r *harvest.Request) {
			r.Provider, r.URLs = "web", []string{"https://u:p@news.mit.edu/a"}
		},
		"no request id": func(r *harvest.Request) { r.RequestID = "" },
	} {
		request := collectionRequest(owner)
		broken(&request)
		if _, err := repo.Request(ctx, request); err == nil {
			t.Fatalf("an invalid collection was posted: %s", name)
		}
	}
}

func TestRequestedCollectionsAreBounded(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewHarvestRepo(db).WithPanel(nil)
	for name, in := range map[string]harvest.Requested{
		"no provider":   {Limit: 10},
		"no limit":      {Provider: "crossref"},
		"absurd limit":  {Provider: "crossref", Limit: 10000},
		"empty request": {},
	} {
		if _, err := repo.Requested(t.Context(), in); err == nil {
			t.Fatalf("an unbounded listing was accepted: %s", name)
		}
	}
	if _, err := repo.Progress(t.Context(), harvest.Progress{ScopeID: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("a collection was read without an owner")
	}
}
