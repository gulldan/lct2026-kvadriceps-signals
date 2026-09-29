package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"
)

type liveAnalyzerFunc func(context.Context, LiveAnalysisRequest) (LiveAnalysisResult, error)

func (f liveAnalyzerFunc) Analyze(ctx context.Context, r LiveAnalysisRequest) (LiveAnalysisResult, error) {
	return f(ctx, r)
}

func TestLiveAnalysisRefreshPreservesScienceAndRejectsFailedReplacement(t *testing.T) {
	snapshot := LiveSnapshot{Version: liveVersion, Method: liveMethod, ObservedAt: "2026-09-10T12:00:00Z", Categories: "fixed",
		Periods: livePeriods(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)),
		Topics: []LiveTopic{{ID: "a", PaperIDs: []string{"p"}, Comparison: &LiveComparison{Current: 1},
			Semantic: &LiveSemantic{Distance: .5}}},
		Papers: []StudyPaper{{ID: "p", Title: "A paper", Date: "2026-09-01"}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	store := &channelTestStore{raw: raw}
	service := NewTrendLiveService(store, func(context.Context) string { return "" })
	service.SetAnalyzer(liveAnalyzerFunc(func(_ context.Context, request LiveAnalysisRequest) (LiveAnalysisResult, error) {
		if request.Discover || !request.EnrichMetadata {
			t.Fatal("analysis-only refresh collected a new population")
		}
		return LiveAnalysisResult{Version: LiveAnalysisVersion,
			Topics:    map[string]LiveTopicAnalysis{"a": {Version: LiveAnalysisVersion}},
			Semantics: map[string]*LiveSemantic{"a": {Distance: .2}}}, nil
	}))
	if err := service.RefreshAnalysis(t.Context()); err != nil {
		t.Fatal(err)
	}
	var got LiveSnapshot
	if err := json.Unmarshal(store.raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ObservedAt != snapshot.ObservedAt || !reflect.DeepEqual(got.Papers, snapshot.Papers) ||
		!reflect.DeepEqual(got.Topics[0].Comparison, snapshot.Topics[0].Comparison) || got.Topics[0].Semantic.Distance != .2 {
		t.Fatalf("science changed or measurement not published %+v", got)
	}
	before := slices.Clone(store.raw)
	service.SetAnalyzer(liveAnalyzerFunc(func(_ context.Context, request LiveAnalysisRequest) (LiveAnalysisResult, error) {
		request.Snapshot.Papers[0].Title = "bad"
		return LiveAnalysisResult{}, errors.New("model unavailable")
	}))
	if err := service.RefreshAnalysis(t.Context()); err == nil || !slices.Equal(before, store.raw) ||
		service.snapshot.Papers[0].Title != "A paper" {
		t.Fatal("failed analysis damaged the successful observation")
	}
}

func TestLiveAnalysisSourceRecoveryOnlyUpdatesDependentEvidence(t *testing.T) {
	metadata := map[string]LivePaperMetadata{}
	snapshot := LiveSnapshot{Periods: []LivePeriod{{Start: "2026-08-01", EndExclusive: "2026-09-11"}}, PaperMetadata: metadata,
		Topics: []LiveTopic{{ID: "a", Series: []StudyPeriod{{Period: "2026-08", Count: 3}}, Semantic: &LiveSemantic{Distance: .4},
			Analysis: &LiveTopicAnalysis{Version: LiveAnalysisVersion, Evidence: LiveIndependence{RecordsExamined: 0},
				Burst: LiveBurst{Flag: new(true)}, Discovery: &LiveDiscovery{ID: "a", PaperIDs: []string{"p"}}},
			Web: &LiveChannel{ObservedAt: "2026-09-01",
				Records: []LiveSourceRecord{{ID: "one", URL: "https://news.example/one", Date: "2026-08-01"},
					{ID: "two", URL: "https://news.example/two", Date: "2026-08-02", OriginalURL: "https://news.example/one",
						OriginalBasis: "isBasedOn"}}}}}}
	oldAnalysis, oldSemantic := snapshot.Topics[0].Analysis, snapshot.Topics[0].Semantic
	encoder := &liveMeaningEncoder{}
	service := TrendLiveService{}
	service.SetAnalyzer(liveAnalyzerFunc(func(ctx context.Context, req LiveAnalysisRequest) (LiveAnalysisResult, error) {
		if !req.EvidenceOnly || req.EnrichMetadata || req.Discover {
			t.Fatal("source recovery requested scientific processing")
		}
		result, err := AnalyzeLive(ctx, encoder, req)
		if len(result.Topics) != 0 || len(result.Semantics) != 0 || len(result.Discoveries) != 0 || result.Metadata != nil {
			t.Fatal("evidence-only returned unrelated updates")
		}
		return result, err
	}))
	if err := service.enrichSourceEvidence(t.Context(), &snapshot); err != nil {
		t.Fatal(err)
	}
	got := snapshot.Topics[0]
	if len(encoder.texts) != 0 || got.Analysis.Evidence.DistinctEvents != 1 || got.Analysis.Evidence.RecordsExamined != 2 ||
		oldAnalysis.Evidence.RecordsExamined != 0 ||
		got.Semantic != oldSemantic || !reflect.DeepEqual(got.Analysis.Discovery, oldAnalysis.Discovery) ||
		!reflect.DeepEqual(got.Analysis.Burst, oldAnalysis.Burst) || got.Series[0].Count != 3 {
		t.Fatal("source recovery retained stale evidence or changed scientific analysis")
	}
}

func TestLiveAnalysisComparisonFailureCannotBecomeSuccessfulReplacement(t *testing.T) {
	previous := &LiveSemantic{Distance: .4}
	request := LiveAnalysisRequest{Snapshot: LiveSnapshot{
		Periods: []LivePeriod{{EndExclusive: "2026-09-11"}},
		Topics:  []LiveTopic{{ID: "a", PaperIDs: []string{"a"}, Semantic: previous}, {ID: "b", PaperIDs: []string{"b"}}},
		Papers:  []StudyPaper{{ID: "a", Title: "One", Date: "2026-08-01"}, {ID: "b", Title: "Two", Date: "2026-08-02"}},
	}}
	for _, encoder := range []*semanticTestEncoder{
		{err: errors.New("model unavailable")},
		{batch: LiveEmbeddingBatch{Model: "model", Revision: "fixed", Vectors: [][]float64{{1, 0}}}},
		{batch: LiveEmbeddingBatch{Model: "model", Revision: "fixed", Vectors: [][]float64{{0, 0}, {1, 0}}}},
	} {
		if _, err := AnalyzeLive(t.Context(), encoder, request); err == nil {
			t.Fatal("comparison failure was reported as a successful analysis")
		}
		if request.Snapshot.Topics[0].Semantic != previous {
			t.Fatal("failed replacement changed the caller's last successful measurement")
		}
	}
}

func TestLiveAnalysisEnrichmentRetainsSampledDiscoveryExclusion(t *testing.T) {
	topic := LiveTopic{ID: "sample", Analysis: &LiveTopicAnalysis{Discovery: &LiveDiscovery{
		ID: "sample", Method: liveSemanticDiscoveryMethod, Sampled: true,
		History: []LiveLineageEvent{{Kind: "rename", At: "2026-09-11", Name: "Current name"}},
	}}}
	request := LiveAnalysisRequest{Snapshot: LiveSnapshot{Categories: "same", Periods: []LivePeriod{},
		Discovered: []LiveTopic{topic}}}
	for month := 2; month <= 8; month++ {
		id := fmt.Sprintf("2026-%02d", month)
		request.Snapshot.Periods = append(request.Snapshot.Periods,
			LivePeriod{ID: id, Start: id + "-01", EndExclusive: fmt.Sprintf("2026-%02d-01", month+1), Complete: true,
				Population: 100})
		count := 1
		if month == 8 {
			count = 20
		}
		request.Snapshot.Discovered[0].Series = append(request.Snapshot.Discovered[0].Series,
			StudyPeriod{Period: id, Count: count})
	}
	result, err := AnalyzeLive(t.Context(), nil, request)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Topics["sample"]
	if got.Discovery == nil || !reflect.DeepEqual(got.Discovery, topic.Analysis.Discovery) ||
		got.Burst.Status != "sampled_discovery" || got.Burst.Flag != nil || got.Burst.Score != nil {
		t.Fatal("enrichment promoted sampled counts to a measured burst or lost lineage")
	}
}

func TestLiveGitHubLineageCacheSurvivesRefreshAndPrunesRemovedRefs(t *testing.T) {
	papers := []StudyPaper{{ID: "p", Title: "New title"}}
	previous := map[string]LivePaperMetadata{
		"p":                               {Abstract: "Old abstract"},
		"https://github.com/kept/repo":    {LineageCheckedAt: "2026-09-11T10:00:00Z", LineageStatus: "provider_linked"},
		"https://github.com/removed/repo": {LineageCheckedAt: "2026-09-11T10:00:00Z"},
	}
	metadata := liveMergeMetadata(map[string]LivePaperMetadata{"p": {Abstract: "New abstract"}}, previous, papers)
	request := PrepareLiveAnalysisRequest(LiveAnalysisRequest{Metadata: metadata, Snapshot: LiveSnapshot{Papers: papers,
		Topics: []LiveTopic{{External: []StudyEvent{{URL: "https://github.com/kept/repo"}}}}}})
	if len(request.Metadata) != 2 || request.Metadata["p"].Abstract != "New abstract" ||
		request.Metadata["https://github.com/kept/repo"].LineageStatus != "provider_linked" {
		t.Fatal("scientific refresh discarded ref cache or retained removed refs")
	}
}

func TestLiveMetadataEnrichmentReplacesOnlyTransientLimits(t *testing.T) {
	retained := []LiveLimit{{Text: "discovery: sample of population"}, {Text: "population coverage warning"},
		{Text: "source unavailable"}}
	snapshot := LiveSnapshot{Limits: append(slices.Clone(retained),
		LiveLimit{Text: "affiliations: old budget"}, LiveLimit{Text: "github lineage: old budget"})}
	service := &TrendLiveService{}
	for pass := range 2 {
		fresh := []LiveLimit{{Text: fmt.Sprintf("affiliations: %d unchecked", 9-pass)},
			{Text: fmt.Sprintf("github lineage: %d unchecked", 5-pass)}}
		service.SetAnalyzer(liveAnalyzerFunc(func(_ context.Context, request LiveAnalysisRequest) (LiveAnalysisResult, error) {
			if !request.EnrichMetadata {
				t.Fatal("missing metadata enrichment request")
			}
			return LiveAnalysisResult{Metadata: map[string]LivePaperMetadata{}, Limits: fresh}, nil
		}))
		if err := service.enrichAnalysis(t.Context(), &snapshot, "", true); err != nil ||
			!sameLimits(snapshot.Limits, slices.Concat(retained, fresh)) {
			t.Fatalf("stale metadata limits accumulated or population limits changed: %v / %v", snapshot.Limits, err)
		}
	}
	previous := slices.Clone(snapshot.Limits)
	service.SetAnalyzer(liveAnalyzerFunc(func(context.Context, LiveAnalysisRequest) (LiveAnalysisResult, error) {
		return LiveAnalysisResult{Limits: []LiveLimit{{Text: "comparison: limited profile"}}}, nil
	}))
	if err := service.enrichAnalysis(t.Context(), &snapshot, "", false); err != nil ||
		!sameLimits(snapshot.Limits, append(previous, LiveLimit{Text: "comparison: limited profile"})) {
		t.Fatal("comparison without metadata erased provider coverage")
	}
	before, _ := json.Marshal(snapshot)
	service.SetAnalyzer(liveAnalyzerFunc(func(context.Context, LiveAnalysisRequest) (LiveAnalysisResult, error) {
		return LiveAnalysisResult{}, errors.New("analysis unavailable")
	}))
	err := service.enrichAnalysis(t.Context(), &snapshot, "", true)
	after, _ := json.Marshal(snapshot)
	if err == nil || string(before) != string(after) {
		t.Fatal("failed request changed prior metadata limits")
	}
}

// Values — карта, поэтому оговорки не сравниваются оператором равенства.
func sameLimits(a, b []LiveLimit) bool {
	return slices.EqualFunc(a, b, func(x, y LiveLimit) bool {
		return x.Code == y.Code && x.Text == y.Text && maps.Equal(x.Values, y.Values)
	})
}
