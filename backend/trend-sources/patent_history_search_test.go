package main

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func historicalPatentFixture(t *testing.T) patentSearchResponse {
	t.Helper()
	raw, err := os.ReadFile("testdata/uspto-public-search-history-sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var response patentSearchResponse
	if err = json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Found != 30289 || len(response.Patents) != 3 {
		t.Fatal("unexpected pinned historical sample")
	}
	return response
}

func TestPublicSearchRowsDoNotRequireGazetteMembership(t *testing.T) {
	fixture := historicalPatentFixture(t)
	data := &snapshot{}
	index := map[string]int{}
	seen := map[string]bool{}
	for _, found := range fixture.Patents {
		if err := applyPatentWeekPage(data, index, found.Published[:10], seen,
			[]patentSearchRecord{found}, provenance{URL: "https://ppubs.uspto.gov/api/searches/searchWithBeFamily"},
			"2026-09-11T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if len(data.Records) != 3 {
		t.Fatal("PPS-only records discarded", data.Records)
	}
	for _, row := range data.Records {
		if row.Title == "" || row.DateBasis != patentSearchDateBasis || row.Text != "" || !metadataVerified(row) {
			t.Fatal("fabricated Gazette/text basis or missing title", row)
		}
	}
	found := fixture.Patents[0]
	found.FamilyID = nil
	found.Application = ""
	found.Priority = nil
	row, err := applyPatentMetadata(newPublicSearchPatent(normalizedPatentID(found.Number), found.Published[:10]),
		found, provenance{}, "2026-09-11T15:00:00Z")
	if err != nil || row.FamilyID != "" || row.FamilyBasis != "unknown" || row.Priority != "" {
		t.Fatal(row, err)
	}
	found.Kind = []string{"C1"}
	found.GUID = "US-" + found.Number + "-C1"
	found.DocumentID = "US " + found.Number + " C1"
	row, err = applyPatentMetadata(newPublicSearchPatent(normalizedPatentID(found.Number), found.Published[:10]),
		found, provenance{}, "2026-09-11T15:00:00Z")
	if err != nil || !metadataVerified(row) || !strings.Contains(row.ExcludedReason, "C1") {
		t.Fatal("certificate accepted as new grant", row, err)
	}
}

func TestHistoricalSweepResumesPinnedPrefixAndLeavesBudgetStopPartial(t *testing.T) {
	fixture := historicalPatentFixture(t)
	requests := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Start int `json:"start"`
			Query struct {
				Q string `json:"q"`
			} `json:"query"`
		}
		if err := json.UnmarshalRead(r.Body, &request); err != nil {
			t.Error(err)
		}
		if request.Query.Q != "@PD>=20250801<=20250831" {
			t.Error("scope changed", request.Query.Q)
		}
		requests = append(requests, request.Start)
		if request.Start > 1 {
			t.Error("unbounded request", request.Start)
			return
		}
		_ = json.MarshalWrite(w, patentSearchResponse{Found: 2, Patents: fixture.Patents[request.Start : request.Start+1]})
	}))
	defer server.Close()
	dir := t.TempDir()
	makeData := func() (*snapshot, patentMetadataProgress, map[int]bool) {
		data := &snapshot{From: "2025-08-01", Through: "2025-08-31", PatentIndexFrom: "2025-09-16",
			ObservedAt: "2026-09-11T15:00:00Z", Coverage: []coverage{{Source: patentSource, From: "2025-08-01",
				Through: "2025-08-31"}}}
		progress := patentMetadataProgress{}
		complete := preparePatentHistory(data, &progress)
		return data, progress, complete
	}
	run := func() (*service, *patentSearchClient) {
		s := &service{dir: dir, config: sourceConfig{RequestBudget: 1}, http: server.Client()}
		return s, &patentSearchClient{service: s, http: server.Client(), base: server.URL, token: "anonymous", caseID: 1}
	}
	data, progress, complete := makeData()
	s, client := run()
	err := s.completePatentHistory(t.Context(), client, data, map[string]int{}, &progress, complete)
	if !errors.Is(err, errBudget) || len(data.Records) != 1 || data.Coverage[0].Exhausted {
		t.Fatal(err, data.Coverage)
	}
	updatePatentProgress(&progress, data, err.Error(), s.requests)
	if progress.Pending != 0 || progress.Exhausted || progress.HistoryWindows[0].Received != 1 {
		t.Fatal(progress)
	}
	// Строки в памяти выброшены: префикс должны восстановить проверенные сырые страницы.
	data, progress, complete = makeData()
	s, client = run()
	if err = s.completePatentHistory(t.Context(), client, data, map[string]int{}, &progress, complete); err != nil {
		t.Fatal(err)
	}
	if len(data.Records) != 2 || !data.Coverage[0].Exhausted ||
		!slices.Equal(requests, []int{0, 1}) {
		t.Fatal(requests, data.Coverage)
	}
	updatePatentProgress(&progress, data, "", s.requests)
	if !progress.Exhausted || progress.Records != 2 || *data.Coverage[0].Total != 2 {
		t.Fatal(progress)
	}
	data, progress, complete = makeData()
	s, client = run()
	s.config.RequestBudget = 0
	err = s.completePatentHistory(t.Context(), client, data, map[string]int{}, &progress, complete)
	if err != nil || s.requests != 0 {
		t.Fatal("repeat redownloaded or lost prefix", err, s.requests)
	}
	if len(data.Records) != 2 || len(requests) != 2 {
		t.Fatal("repeat changed identities")
	}
}

func TestHistoryCalendarGapAndLaterArchiveFailure(t *testing.T) {
	for _, complete := range []bool{true, false} {
		data := &snapshot{From: "2025-08-01", Through: "2026-09-11", PatentIndexFrom: "2025-09-16", Coverage: []coverage{
			{Source: patentSource, From: "2025-08-01", Through: "2025-08-31"},
			{Source: patentSource, From: "2025-09-01", Through: "2025-09-30", Exhausted: complete},
		}}
		progress := patentMetadataProgress{}
		archive := preparePatentHistory(data, &progress)
		if len(progress.HistoryWindows) != 2 ||
			progress.HistoryWindows[0].Through != "2025-08-31" ||
			progress.HistoryWindows[1].Through != "2025-09-15" || archive[1] != complete || data.Coverage[1].Exhausted {
			t.Fatal("history gap or archive failure erased", progress, archive)
		}
	}
}

func TestHistoryRejectsDateDriftDuplicatesAndChangedTotals(t *testing.T) {
	fixture := historicalPatentFixture(t)
	for _, failure := range []string{"date", "duplicate", "total"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Start int `json:"start"`
				}
				if err := json.UnmarshalRead(r.Body, &request); err != nil {
					t.Error(err)
				}
				found := fixture.Patents[min(request.Start, 1)]
				total := 2
				if request.Start == 1 {
					switch failure {
					case "date":
						found.Published = "2025-09-02T00:00:00Z"
					case "duplicate":
						found = fixture.Patents[0]
					case "total":
						total = 3
					}
				}
				_ = json.MarshalWrite(w, patentSearchResponse{Found: total, Patents: []patentSearchRecord{found}})
			}))
			defer server.Close()
			s := &service{dir: t.TempDir(), config: sourceConfig{RequestBudget: 2}, http: server.Client()}
			client := &patentSearchClient{service: s, http: server.Client(), base: server.URL, token: "anonymous", caseID: 1}
			data := &snapshot{From: "2025-08-01", Through: "2025-08-31", PatentIndexFrom: "2025-09-16"}
			progress := patentMetadataProgress{}
			preparePatentHistory(data, &progress)
			err := s.patentHistoryRange(t.Context(), client, data, map[string]int{}, &progress, 0)
			if err == nil || progress.HistoryWindows[0].Exhausted {
				t.Fatal("invalid page certified", failure, err)
			}
		})
	}
}
