package application

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/main-service/internal/platform/openalex"
)

func TestLivePriorArtUsesEarlierDatesAndKeepsUncertainty(t *testing.T) {
	t.Setenv("RADAR_PRIOR_ART", "true")
	response := `{"meta":{"count":4},"results":[` +
		`{"id":"https://openalex.org/W122","title":"On the Pathology and Treatment of Ovarian Diseases",` +
		`"type":"article","publication_date":"1874-07-04","abstract_inverted_index":{"Earlier":[0],"Method":[1]}},` +
		`{"id":"https://openalex.org/W123","doi":"https://doi.org/10.1234/earlier","title":"Earlier-method: original result",` +
		`"type":"article","publication_date":"2010-01-01"},` +
		`{"id":"https://openalex.org/W124","title":"Later method","type":"article","publication_date":"2025-01-01"},` +
		`{"id":"javascript:alert(1)","title":"Untrusted link","type":"article","publication_date":"2000-01-01"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter") != `title.search:"Earlier Method",to_publication_date:2023-12-31` ||
			q.Get("sort") != "publication_date:asc" || q.Get("per_page") != "10" {
			t.Error("prior art search changed name, cutoff or oldest-first order")
		}
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()
	client, err := openalex.New(openalex.Config{BaseURL: server.URL, APIKey: "fixture", Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	service := &TrendLiveService{oa: client}
	candidate := LiveTopic{Name: "Название метода", PaperIDs: []string{"new", "old"},
		Qualification: &LiveQualification{CanonicalName: "Earlier Method"}}
	papers := map[string]StudyPaper{"old": {Date: "2024-01-01"}, "new": {Date: "2025-01-01"}}
	ctx := liveWithPriorArt(t.Context())
	if liveWithPriorArt(ctx) != ctx {
		t.Fatal("later wave reset prior art budget")
	}
	got := service.priorArt(ctx, candidate, papers)
	if got.Status != "candidates" || len(got.Works) != 1 || got.Works[0].Date != "2010-01-01" ||
		got.Works[0].URL != "https://doi.org/10.1234/earlier" || got.SourceSHA256 == "" || got.Before != "2024-01-01" ||
		strings.Contains(got.SourceURL, "fixture") || !strings.Contains(got.Note, "требует проверки") {
		t.Fatalf("date filtering or uncertainty lost: %+v", got)
	}
	for _, test := range []struct{ raw, status string }{
		{`{"meta":{"count":0},"results":[]}`, "not_found"},
		{`{"error":"rate limit"}`, "unavailable"},
		{`{"meta":{"count":0},"results":[{}]}`, "unavailable"},
		{`{"meta":{"count":1},"results":[]}`, "unavailable"},
	} {
		response = test.raw
		got = service.priorArt(ctx, candidate, papers)
		if got.Status != test.status || got.Note == "" {
			t.Fatalf("absence and failed lookup conflated: %+v", got)
		}
	}
	budget, ok := ctx.Value(livePriorKey{}).(*livePriorBudget)
	if !ok {
		t.Fatal("missing prior art budget")
	}
	budget.remaining = 0
	if service.priorArt(ctx, candidate, papers).Status != "unavailable" {
		t.Fatal("exhausted query budget claimed no earlier work")
	}
}

func TestLivePriorArtOnlyChecksFinalSelection(t *testing.T) {
	t.Setenv("RADAR_PRIOR_ART", "true")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sort") == "publication_date:asc" {
			calls.Add(1)
		}
		_, _ = io.WriteString(w, `{"meta":{"count":0},"results":[]}`)
	}))
	defer server.Close()
	client, err := openalex.New(openalex.Config{BaseURL: server.URL, APIKey: "fixture", Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, candidates, documents, periods := liveWaveFixture(t, 40)
	service.oa = client
	for i := range candidates {
		candidates[i].Name = "Earlier " + candidates[i].Name
	}
	umbrella := LiveTopic{}
	selected, rejected, _ := service.finishCandidates(t.Context(), "квантовое измерение", &umbrella,
		candidates, documents, nil, periods, nil)
	if len(selected) != liveTopCandidates || calls.Load() != int64(len(selected)) {
		t.Fatalf("prior requests escaped final selection: selected=%d calls=%d", len(selected), calls.Load())
	}
	for _, candidate := range selected {
		if candidate.Card == nil || candidate.Card.PriorArt == nil || candidate.Card.PriorArt.Status != "not_found" {
			t.Fatalf("final card missed earlier-work search: %+v", candidate.Card)
		}
	}
	for _, refusal := range rejected {
		if refusal.topic != nil && refusal.topic.Card != nil && refusal.topic.Card.PriorArt != nil {
			t.Fatal("discarded candidate consumed a prior-art request")
		}
	}
}
