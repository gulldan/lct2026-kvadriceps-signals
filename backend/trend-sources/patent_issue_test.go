package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestGazetteCoverDateAndIndexDirectoryTypo(t *testing.T) {
	cover, err := os.ReadFile("testdata/uspto-week25-cover.html")
	if err != nil {
		t.Fatal(err)
	}
	listed := issue{URL: gazette + "/week25", Date: "2026-06-25"}
	resolved, err := patentIssueDate(cover, listed)
	if err != nil || resolved.Date != "2026-06-23" || resolved.URL != listed.URL {
		t.Fatal(resolved, err)
	}
	index, err := os.ReadFile("testdata/uspto-week25-alphaA.html")
	if err != nil {
		t.Fatal(err)
	}
	if rows, e := patentRows(index, listed); e == nil || len(rows) != 0 {
		t.Fatal("wrong directory label silently certified", len(rows), e)
	}
	if rows, e := patentRows(index, resolved); e != nil || len(rows) == 0 {
		t.Fatal("verified issue lost", len(rows), e)
	}
	stale := strings.ReplaceAll(string(cover), "2026", "2025")
	if _, e := patentIssueDate([]byte(stale), listed); e == nil {
		t.Fatal("recycled weekly URL accepted as this year")
	}
	ambiguous := strings.Replace(string(cover), "June 23, 2026", "June 23, 2026<br>June 24, 2026", 1)
	if _, e := patentIssueDate([]byte(ambiguous), listed); e == nil {
		t.Fatal("conflicting primary cover dates accepted")
	}
	if _, e := patentIssueDate([]byte("Access denied"), listed); e == nil {
		t.Fatal("non-cover page accepted")
	}
}

func TestRealShortGrantTitlesRetainTheirIDs(t *testing.T) {
	raw, err := os.ReadFile("testdata/uspto-week25-short-titles.html")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := patentRows(raw, issue{URL: gazette + "/week25", Date: "2026-06-23"})
	if err != nil || len(rows) != 2 {
		t.Fatal("short valid titles lost", rows, err)
	}
	for _, expected := range []struct{ ID, Title string }{{"USD1131000", "Bag"}, {"US12663059", "Unit"}} {
		if !slices.ContainsFunc(rows, func(row record) bool { return row.ID == expected.ID && row.Title == expected.Title }) {
			t.Fatal("real ID/title not retained", expected)
		}
	}
}

func TestPrimaryIssueCoverCheckpointRetainsDirectoryIdentity(t *testing.T) {
	t.Parallel()
	cover, err := os.ReadFile("testdata/uspto-week25-cover.html")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_,
			_ = w.Write(cover)
	}))
	defer server.Close()
	listed := issue{URL: server.URL + "/week25", Date: "2026-06-25"}
	s := &service{dir: t.TempDir(), http: server.Client(), config: sourceConfig{RequestBudget: 1}}
	first, proof, err := s.resolvePatentIssue(t.Context(), listed, &snapshot{})
	if err != nil || first.Date != "2026-06-23" || proof.SHA256 != hashBytes(cover) || calls != 1 {
		t.Fatal(first, proof, calls, err)
	}
	second, saved, err := s.resolvePatentIssue(t.Context(), listed, &snapshot{})
	if err != nil || second != first || saved != proof || calls != 1 {
		t.Fatal("cover fetched again despite pinned cache", second, calls, err)
	}
}
