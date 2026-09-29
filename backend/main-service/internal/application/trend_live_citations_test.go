package application

import (
	"context"
	"github.com/example/main-service/internal/platform/openalex"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOpenAlexCitationsNormalizeAgainstBranchMonth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "oa-key" || r.URL.Query().Get("per-page") != "100" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		filter := r.URL.Query().Get("filter")
		rows := []string{}
		for _, doi := range strings.Split(strings.TrimPrefix(filter, "doi:"), "|") {
			count := "0"
			if strings.HasSuffix(doi, "2601.00001") {
				count = "12"
			}
			if strings.HasSuffix(doi, "2601.00009") {
				continue // OpenAlex эту работу не знает
			}
			rows = append(rows, `{"doi":"https://doi.org/`+doi+`","cited_by_count":`+count+`}`)
		}
		_, _ = w.Write([]byte(`{"results":[` + strings.Join(rows, ",") + `]}`))
	}))
	defer server.Close()
	src := &liveSources{http: server.Client(), oa: testOpenAlexClient(t, server.URL, "oa-key"),
		metadata: map[string]LivePaperMetadata{},
		sleep:    func(context.Context, time.Duration) error { return nil }, progress: func(string, time.Duration) {}}
	window := []StudyPaper{}
	for i := 1; i <= 9; i++ {
		id := "arxiv:2601.0000" + string(rune('0'+i))
		window = append(window, StudyPaper{ID: id, Date: "2026-01-1" + string(rune('0'+i%10))})
		src.metadata[id] = LivePaperMetadata{Category: "cs.LG"}
	}
	if doi := liveDOI(window[0], src.metadata[window[0].ID]); doi != "10.48550/arxiv.2601.00001" {
		t.Fatalf("doi = %q", doi)
	}
	requests, err := src.openAlexCitations(context.Background(), window, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil || requests != 1 {
		t.Fatalf("requests = %d, err = %v", requests, err)
	}
	if src.metadata["arxiv:2601.00009"].CitedBy != nil || *src.metadata["arxiv:2601.00001"].CitedBy != 12 {
		t.Fatalf("counts: %+v", src.metadata)
	}
	cells := liveCitationCells(window, src.metadata)
	topic := liveTopicCitations(window[:6], src.metadata, cells, "2026-09-14T00:00:00Z")
	if topic == nil || topic.Papers != 6 || topic.Total != 12 || topic.Cited != 1 || topic.Normalized == nil {
		t.Fatalf("topic citations: %+v", topic)
	}
	// Восемь известных работ с 12 цитированиями в клетке: ожидание 1,5 на работу, шесть работ ждут 9,
	// наблюдено двенадцать → 1,33.
	if got := *topic.Normalized; got < 1.32 || got > 1.34 {
		t.Fatalf("normalized = %v", got)
	}
	again, err := src.openAlexCitations(context.Background(), window, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	if err != nil || again != 0 {
		t.Fatalf("fresh counts refetched: %d %v", again, err)
	}
}

func TestOpenAlexBudgetExhaustionFailsFastAndKeepsCachedCounts(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "8246")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	src := &liveSources{http: server.Client(), oa: testOpenAlexClient(t, server.URL, ""), metadata: map[string]LivePaperMetadata{},
		sleep: func(context.Context, time.Duration) error { return nil }, progress: func(string, time.Duration) {}}
	window := []StudyPaper{{ID: "arxiv:2601.00001", Date: "2026-01-11"}, {ID: "arxiv:2601.00002", Date: "2026-01-12"}}
	for _, paper := range window {
		src.metadata[paper.ID] = LivePaperMetadata{Category: "cs.LG"}
	}
	requests, err := src.openAlexCitations(context.Background(), window, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err == nil || requests != 0 || calls != 1 || !strings.Contains(err.Error(), "budget exhausted, resets in 2h17m") ||
		!strings.Contains(err.Error(), "2 papers keep counts") {
		t.Fatalf("requests = %d, calls = %d, err = %v", requests, calls, err)
	}
	store := memoryTestStore{liveCitationCacheKey: []byte(
		`{"arxiv:2601.00001":{"cited_by":4,"cited_at":"2026-09-01T00:00:00Z"},` +
			`"arxiv:2601.00002":{"cited_by":0,"cited_at":"2026-09-01T00:00:00Z"}}`)}
	service := &TrendLiveService{store: store}
	snapshot := &LiveSnapshot{Papers: window, Limits: []LiveLimit{},
		Topics: []LiveTopic{{ID: "t", PaperIDs: []string{window[0].ID, window[1].ID}}}}
	service.collectCitations(context.Background(), src, snapshot, window)
	topic := snapshot.Topics[0].Citations
	if topic == nil || topic.Total != 4 || topic.Cited != 1 || topic.Papers != 2 || snapshot.Citations.Cells != 1 ||
		!slices.ContainsFunc(snapshot.Limits, func(l LiveLimit) bool {
			return strings.Contains(l.Text, "budget exhausted")
		}) {
		t.Fatalf("cached counts must survive an OpenAlex outage: %+v limits=%v", topic, snapshot.Limits)
	}
}

func testOpenAlexClient(t *testing.T, base, key string) *openalex.Client {
	t.Helper()
	client, err := openalex.New(openalex.Config{BaseURL: base, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	client.Sleep = func(context.Context, time.Duration) error { return nil }
	return client
}
