package postgres

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
)

func TestFrozenRequestRetryPreservesExpansion(t *testing.T) {
	original := analysisdoc.Request{
		OwnerID: uuid.NewV4().String(), RequestID: uuid.NewV4().String(), Query: "поисковое расширение",
		Start: "2022-01-01", Cutoff: "2026-09-17T00:00:00Z", Mode: analysisdoc.ModeAsOf,
		AnalysisKind: analysisdoc.KindBriefV1, SharedCorpus: true, SearchTerms: []string{"query expansion"},
		CollectionScopeIDs: []string{strings.Repeat("a", 64)}, TestRunID: "tr-test-" + uuid.NewV4().String(),
	}
	encode := func(value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	manifest := string(encode(analysisdoc.Input{Version: analysisdoc.Version, Request: original}))
	storedHash := indexdoc.Hash(encode(original))
	for _, terms := range [][]string{original.SearchTerms, {"query rewriting", "query expansion"}, nil} {
		retry := original
		retry.SearchTerms = terms
		if err := checkFrozenRequest(retry, manifest, storedHash); err != nil {
			t.Fatalf("same user input with regenerated terms was rejected: %v", err)
		}
	}
	for name, change := range map[string]func(*analysisdoc.Request){
		"owner":   func(r *analysisdoc.Request) { r.OwnerID = uuid.NewV4().String() },
		"key":     func(r *analysisdoc.Request) { r.RequestID = uuid.NewV4().String() },
		"query":   func(r *analysisdoc.Request) { r.Query = "другой запрос" },
		"start":   func(r *analysisdoc.Request) { r.Start = "2023-01-01" },
		"cutoff":  func(r *analysisdoc.Request) { r.Cutoff = "2026-09-16T00:00:00Z" },
		"mode":    func(r *analysisdoc.Request) { r.Mode = analysisdoc.ModeRetrospective },
		"kind":    func(r *analysisdoc.Request) { r.AnalysisKind = "different-contract" },
		"access":  func(r *analysisdoc.Request) { r.SharedCorpus = false },
		"scope":   func(r *analysisdoc.Request) { r.CollectionScopeIDs = []string{strings.Repeat("b", 64)} },
		"test ID": func(r *analysisdoc.Request) { r.TestRunID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			retry := original
			retry.SearchTerms = []string{"query rewriting"}
			change(&retry)
			if err := checkFrozenRequest(retry, manifest, storedHash); !errors.Is(err, analysisdoc.ErrConflict) {
				t.Fatalf("changed user input must conflict: %v", err)
			}
		})
	}
	if err := checkFrozenRequest(original, "{", storedHash); err == nil {
		t.Fatal("invalid stored manifest accepted")
	}
}
