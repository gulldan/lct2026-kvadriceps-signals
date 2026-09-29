package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchWindowTranslatesAndMatchesLocally(t *testing.T) {
	s := NewTrendLiveService(nil, func(context.Context) string { return "" })
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	s.setWindow([]StudyPaper{
		{ID: "arxiv:1", Title: "Solid oxide electrolysis at scale", Date: "2026-03-10"},
		{ID: "arxiv:2", Title: "Unrelated", Date: "2026-03-11"},
		{ID: "arxiv:3", Title: "Hydrogen", Date: "2026-03-12"},
		{ID: "arxiv:4", Title: "Old electrolysis", Date: "2020-01-01"},
	}, map[string]LivePaperMetadata{"arxiv:3": {Abstract: "Water electrolysis with a solid oxide cell."}})
	var asked string
	s.SetPlanner(liveTestQueryPlanner(func(_ context.Context, query string) ([]string, error) {
		asked = query
		return []string{"solid oxide electrolysis", "electrolysis", "solid oxide cell"}, nil
	}))
	result, err := s.SearchWindow(t.Context(), LiveWindowSearch{Query: "  Твердооксидный   электролиз ", Periods: periods})
	if err != nil {
		t.Fatal(err)
	}
	if asked != "твердооксидный электролиз" || result.Expression != `all:"solid oxide electrolysis" OR all:"solid oxide cell"` {
		t.Fatalf("translation: %q %q", asked, result.Expression)
	}
	if result.Total != 2 || len(result.Papers) != 2 || result.Papers[0].ID != "arxiv:1" || result.Papers[1].ID != "arxiv:3" {
		t.Fatalf("matches: %+v", result)
	}
	if _, ok := result.Metadata["arxiv:3"]; !ok || len(result.Limits) != 0 {
		t.Fatalf("metadata and limits: %+v", result)
	}

	// Без разбора области запрос не превращается в готовый поиск по неявным критериям.
	s.SetPlanner(func(context.Context, string) (LiveQueryPlan, error) {
		return LiveQueryPlan{}, errors.New("gateway down")
	})
	result, err = s.SearchWindow(t.Context(), LiveWindowSearch{Query: "электролиз", Periods: periods})
	if err == nil || result.Scope != nil || len(result.Papers) != 0 {
		t.Fatalf("planner failure produced an implicit scope: %v %+v", err, result)
	}
	s.SetPlanner(nil)
	if _, err = s.SearchWindow(t.Context(), LiveWindowSearch{Query: "electrolysis", Periods: periods}); err == nil {
		t.Fatal("missing planner silently accepted an unscoped search")
	}
	plan := liveFixtureQueryPlan("electrolysis", []string{"electrolysis"})
	if result, err = s.SearchWindow(t.Context(), LiveWindowSearch{Query: "electrolysis", Periods: periods,
		Plan: &plan}); err != nil || result.Total != 2 {
		t.Fatalf("prepared English plan: %v %+v", err, result)
	}
	empty := NewTrendLiveService(nil, nil)
	_, err = empty.SearchWindow(t.Context(), LiveWindowSearch{Query: "x", Periods: periods})
	if !errors.Is(err, ErrLiveWindowEmpty) {
		t.Fatalf("empty window: %v", err)
	}
}

func TestSearchWindowNormalizesEnglishRequests(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	s.setWindow([]StudyPaper{
		{ID: "graph", Title: "Federated graph learning for financial fraud", Date: "2026-03-10"},
		{ID: "privacy", Title: "Private financial networks", Date: "2026-04-10"},
		{ID: "other", Title: "Materials synthesis", Date: "2026-05-10"},
	}, map[string]LivePaperMetadata{"privacy": {Abstract: "A method for cross-bank fraud detection."}})
	const query = "federated graph learning for cross-bank money laundering detection without sharing transactions"
	terms := []string{"federated graph learning", "cross-bank fraud detection"}
	var calls int
	s.SetPlanner(liveTestQueryPlanner(func(_ context.Context, got string) ([]string, error) {
		calls++
		if got != query && got != "federated" {
			t.Errorf("normalizer received %q", got)
		}
		return terms, nil
	}))
	result, err := s.SearchWindow(t.Context(), LiveWindowSearch{Query: query, Periods: periods})
	if err != nil || !slices.Equal(result.Terms, terms) || result.Total != 2 || len(result.Papers) != 2 ||
		result.Papers[0].ID != "graph" || result.Papers[1].ID != "privacy" {
		t.Fatalf("English prose was not normalized into matching phrases: %+v, %v", result, err)
	}
	family, planErr := s.searchFamily(t.Context(), query)
	if family.query.Arxiv != result.Expression || !slices.Equal(family.query.Terms, terms) || planErr != nil || calls != 2 {
		t.Fatalf("collector and public fallback use different terms: %+v, %v, %d", family.query, planErr, calls)
	}
	result, err = s.SearchWindow(t.Context(), LiveWindowSearch{Query: "federated", Periods: periods})
	family, planErr = s.searchFamily(t.Context(), "federated")
	if err != nil || result.Total != 2 || family.query.Arxiv != result.Expression || planErr != nil || calls != 4 {
		t.Fatalf("single English direction lost its subareas: %+v, %v", result, err)
	}
}

func TestWindowFeaturesKeepTrainingPhraseWithoutPlanning(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	const phrase = "lithium projected phonon density of states descriptor"
	form := weakSignalWindowPhrase(phrase)
	s.setWindow([]StudyPaper{
		{ID: "title", Title: "Lithium projected phonon density of states descriptor for batteries", Date: "2026-03-10"},
		{ID: "abstract", Title: "Battery conductivity", Date: "2026-04-10"},
		{ID: "unrelated", Title: "Battery electrolyte synthesis", Date: "2026-05-10"},
		{ID: "old", Title: "Lithium projected phonon density of states descriptor", Date: "2020-01-01"},
	}, map[string]LivePaperMetadata{"abstract": {Abstract: "We evaluate the " + phrase + "."}})
	var calls atomic.Int32
	s.SetPlanner(liveTestQueryPlanner(func(context.Context, string) ([]string, error) {
		calls.Add(1)
		return []string{"battery"}, nil
	}))
	const absent = "unreported cathode pathway"
	windows, err := s.WindowFeatures(t.Context(), LiveFeatureRequest{
		Phrases: []string{form, absent}, Periods: periods,
	})
	if err != nil || calls.Load() != 0 {
		t.Fatalf("feature collection must not plan or expand phrases: calls=%d, error=%v", calls.Load(), err)
	}
	window, found := windows[form]
	if !found || len(windows) != 2 || window.Total != 2 || window.Documents != 2 || window.ScientificWorks != 2 ||
		window.ActivePeriods != 2 || !slices.Equal(window.Titles, []string{
		LiveRatingTitle("Battery conductivity", "2026-04-10"),
		LiveRatingTitle("Lithium projected phonon density of states descriptor for batteries", "2026-03-10"),
	}) {
		t.Fatalf("literal title/abstract matches, periods or newest titles were lost: %+v", windows)
	}
	if missing, measured := windows[absent]; !measured || missing.Total != 0 || missing.Documents != 0 {
		t.Fatalf("an absent phrase must have a measured zero, not a missing window: %+v", windows)
	}
	result, err := s.SearchWindow(t.Context(), LiveWindowSearch{Query: form, Periods: periods})
	if err != nil || calls.Load() != 1 || result.Total != 3 || !slices.Equal(result.Terms, []string{"battery"}) {
		t.Fatalf("user searches must retain their planner: calls=%d, result=%+v, error=%v", calls.Load(), result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	windows, err = s.WindowFeatures(ctx, LiveFeatureRequest{Phrases: []string{form}, Periods: periods})
	if !errors.Is(err, context.Canceled) || len(windows) != 0 || calls.Load() != 1 {
		t.Fatalf("canceled feature collection must not invoke a planner: windows=%+v, error=%v", windows, err)
	}
}

func TestSearchWindowCarriesRunIDAcrossCollector(t *testing.T) {
	collector := NewTrendLiveService(nil, nil)
	collector.setWindow([]StudyPaper{{ID: "one", Title: "Graph learning", Date: "2026-05-10"}}, nil)
	received := make(chan string, 1)
	prepared := make(chan LiveWindowSearch, 1)
	collector.SetPlanner(liveTestQueryPlanner(func(ctx context.Context, _ string) ([]string, error) {
		id, _ := ctx.Value(liveLLMRunKey{}).(string)
		received <- id
		return []string{"graph learning"}, nil
	}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request LiveWindowSearch
		if err := json.UnmarshalRead(r.Body, &request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Plan != nil {
			prepared <- request
		}
		if _, err := collector.SearchWindow(r.Context(), request); err != nil {
			t.Error(err)
		}
		// После проверки передачи контекста внешнее обогащение не запускается.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	s := &TrendLiveService{collectorURL: server.URL}
	src := &liveSources{http: server.Client()}
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	for _, tc := range []struct{ sent, want string }{
		{"run-provenance-example", "run-provenance-example"}, {"bad\nidentifier", ""}, {"", ""},
	} {
		ctx := context.WithValue(t.Context(), liveLLMRunKey{}, tc.sent)
		_, _, err := s.searchWindow(ctx, src, liveFamily{name: "graph based learning"}, periods)
		if !errors.Is(err, errLiveCollectorUnavailable) {
			t.Fatalf("fixture must stop before external enrichment: %v", err)
		}
		select {
		case got := <-received:
			if got != tc.want {
				t.Fatalf("collector context run ID = %q, want %q", got, tc.want)
			}
		default:
			t.Fatal("collector did not normalize the request")
		}
	}
	// Готовый план пересекает HTTP-границу без второго вызова модели в сборщике.
	plan := liveFixtureQueryPlan("graph based learning", []string{"graph learning"})
	ctx := context.WithValue(t.Context(), liveLLMRunKey{}, "prepared-plan-run")
	_, _, err := s.searchWindow(ctx, src, liveFamily{name: "graph based learning", plan: &plan}, periods)
	if !errors.Is(err, errLiveCollectorUnavailable) {
		t.Fatalf("fixture must stop before enrichment: %v", err)
	}
	select {
	case request := <-prepared:
		if request.RunID != "prepared-plan-run" || !liveSameQueryScope(request.Plan.Scope, plan.Scope) ||
			!slices.Equal(request.Plan.Terms, plan.Terms) {
			t.Fatalf("collector received a different plan: %+v", request)
		}
	default:
		t.Fatal("prepared plan was not sent to the collector")
	}
	select {
	case <-received:
		t.Fatal("collector reinterpreted an already planned request")
	default:
	}
}

func TestParseSearchTermsIsForgiving(t *testing.T) {
	got := ParseSearchTerms(`Sure! ["Solid Oxide Electrolysis", "SOEC", "solid oxide electrolysis",
 "a very long phrase that has too many words in it", "x", "y", "z"]`, 4)
	if len(got) != 4 || got[0] != "solid oxide electrolysis" || got[1] != "soec" || got[2] != "x" {
		t.Fatalf("terms: %q", got)
	}
	long := ParseSearchTerms(`["infrastructure for training large ai models", "llm inference", "gpu clusters"]`,
		LiveSearchTerms)
	if len(long) != 3 || long[0] != "llm inference" || long[2] != "infrastructure for training large ai models" {
		t.Fatalf("short phrases must come first: %q", long)
	}
	if ParseSearchTerms("no json here", LiveSearchTerms) != nil {
		t.Fatal("garbage must yield nothing")
	}
	covered := liveDropCoveredWords([]string{"real world assets", "tokenization", "asset tokenization", "soec"})
	if !slices.Equal(covered, []string{"real world assets", "asset tokenization", "soec"}) {
		t.Fatalf("a bare word inside another phrase must go: %q", covered)
	}
}
