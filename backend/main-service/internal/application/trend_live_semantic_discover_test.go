package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type liveMeaningEncoder struct{ texts []string }

func (e *liveMeaningEncoder) Encode(_ context.Context, texts []string) (LiveEmbeddingBatch, error) {
	e.texts = slices.Clone(texts)
	result := LiveEmbeddingBatch{Model: "test-semantic", Revision: "fixed"}
	for _, text := range texts {
		vector := []float64{1, 0, 0}
		if strings.Contains(text, "electrochemical") {
			vector = []float64{0, 1, 0}
		}
		if strings.Contains(text, "optical") {
			vector = []float64{0, 0, 1}
		}
		result.Vectors = append(result.Vectors, vector)
	}
	return result, nil
}

func TestLiveSemanticDiscoveryFormsMembershipBeforeNamingAndExcludesFuture(t *testing.T) {
	req := LiveAnalysisRequest{Snapshot: LiveSnapshot{Periods: livePeriods(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)),
		Categories: "fixed"},
		Metadata: map[string]LivePaperMetadata{}, Discover: true, StrictAvailability: true}
	for i := range 18 {
		id := fmt.Sprintf("p%02d", i)
		title, abstract := "Agent harness evaluation", "autonomous software agent test runner"
		if i%3 == 0 {
			title = "Execution scaffold reliability"
		}
		if i >= 6 && i < 12 {
			title, abstract = "Battery method evaluation", "electrochemical solid state electrolyte"
		}
		if i >= 12 {
			title, abstract = "Quantum device evaluation", "optical single photon sensing"
		}
		req.DiscoveryPapers = append(req.DiscoveryPapers, StudyPaper{ID: id, Title: title, Date: "2026-08-10"})
		req.Metadata[id] = LivePaperMetadata{Abstract: abstract, AvailableAt: "2026-08-10"}
	}
	req.DiscoveryPapers = append(req.DiscoveryPapers,
		StudyPaper{ID: "future", Title: "Agent future instruction", Date: "2026-09-11"},
		StudyPaper{ID: "later-version", Title: "Agent later revision", Date: "2026-08-10"},
		StudyPaper{ID: "unknown", Title: "Unknown timestamp", Date: "2026-08-10"})
	req.Metadata["future"] = LivePaperMetadata{AvailableAt: "2026-09-11"}
	req.Metadata["later-version"] = LivePaperMetadata{AvailableAt: "2026-09-11"}
	encoder := &liveMeaningEncoder{}
	result, _, err := liveSemanticDiscover(t.Context(), encoder, req)
	if err != nil || len(result) != 3 {
		t.Fatalf("semantic groups: %+v, %v", result, err)
	}
	for _, group := range result {
		if len(group.PaperIDs) != 6 || !strings.HasPrefix(group.ID, "semantic:") || group.Method != liveSemanticDiscoveryMethod {
			t.Fatalf("unexpected group %+v", group)
		}
		if slices.Contains(group.PaperIDs, "p00") && !slices.Contains(group.PaperIDs, "p01") {
			t.Fatal("synonyms were divided by n-gram surface")
		}
		if len(group.History) != 1 || group.History[0].At != "2026-09-10" ||
			group.History[0].DateBasis != "cutoff-inclusive-date-v1" {
			t.Fatalf("creation was dated at the exclusive publication boundary: %+v", group.History)
		}
	}
	if len(encoder.texts) != 18 {
		t.Fatalf("future/unavailable version leaked: %v", encoder.texts)
	}
	req.Snapshot.Discovered = []LiveTopic{{ID: result[0].ID, Name: "Old label", PaperIDs: []string{"obsolete"}}}
	req.Previous = result
	analysis, err := AnalyzeLive(t.Context(), encoder, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := analysis.Semantics[result[0].ID]; got == nil ||
		slices.Contains(got.PaperIDs, "obsolete") || len(got.PaperIDs) != 6 {
		t.Fatalf("reused ID retained obsolete membership: %+v", got)
	}
	req.Previous = nil
	req.Snapshot.Discovered = nil
	slices.Reverse(req.DiscoveryPapers)
	second, _, err := liveSemanticDiscover(t.Context(), encoder, req)
	if err != nil || !reflect.DeepEqual(result, second) {
		t.Fatal("input order changed deterministic discovery")
	}
}

func TestLiveSemanticSplitLabelsUseSupportedTechnicalPhrases(t *testing.T) {
	topics := []LiveDiscovery{{ID: "a", Name: "Policy Distillation", Aliases: []string{"Policy Distillation", "Trust Region"},
		PaperIDs: []string{"a"}},
		{ID: "b", Name: "Policy Distillation", Aliases: []string{"Policy Distillation", "Token Sampling"},
			PaperIDs: []string{"b"}}}
	liveDiscoveryLabels(topics,
		[]StudyPaper{{ID: "a", Title: "Trust Region Policy Distillation"},
			{ID: "b", Title: "Token Sampling Policy Distillation"}})
	if topics[0].Name == topics[1].Name || !strings.Contains(topics[0].Name, "Trust Region") ||
		!strings.Contains(topics[1].Name, "Token Sampling") || topics[0].Aliases[0] != "Policy Distillation" {
		t.Fatalf("unsupported or indistinguishable labels %+v", topics)
	}
}

func TestLiveAnalysisSamplingPreservesDomainsAndInput(t *testing.T) {
	papers := make([]StudyPaper, 0, 20)
	metadata := map[string]LivePaperMetadata{}
	for i := range 20 {
		id := strconv.Itoa(i)
		papers = append(papers, StudyPaper{ID: id, Title: id, Date: "2026-08-10"})
		metadata[id] = LivePaperMetadata{Category: "IT", Abstract: "Unversioned abstract must not leak"}
	}
	metadata["0"] = LivePaperMetadata{Category: "energy"}
	first := liveStratifiedPapers(papers, metadata, 4)
	slices.Reverse(papers)
	second := liveStratifiedPapers(papers, metadata, 4)
	if !reflect.DeepEqual(first, second) || !slices.ContainsFunc(first, func(p StudyPaper) bool { return p.ID == "0" }) {
		t.Fatalf("biased/unstable sample %v/%v", first, second)
	}
	req := LiveAnalysisRequest{Snapshot: LiveSnapshot{Periods: []LivePeriod{{Start: "2026-08-01", EndExclusive: "2026-09-11"}}},
		DiscoveryPapers: papers, Metadata: metadata}
	_, _ = liveEligibleDiscovery(req)
	if metadata["1"].Abstract == "" {
		t.Fatal("analysis mutated caller metadata")
	}
}

func TestLiveSemanticLineageRenamesMergesSplitsAndRejectsFuture(t *testing.T) {
	old := []LiveDiscovery{{ID: "stable-a", Name: "Agent Harness", PaperIDs: []string{"a", "b", "c", "d"},
		History: []LiveLineageEvent{{Kind: "created", At: "2026-08-01"}}},
		{ID: "stable-b", Name: "Agent Harnesses", PaperIDs: []string{"e", "f"}}}
	merged := []LiveDiscovery{{ID: "new", Name: "Agent Scaffolds", PaperIDs: []string{"a", "b", "c", "d", "e", "f"}}}
	liveDiscoveryLineage(merged, old, "2026-09-11", "snapshot-observed-date-v1")
	if merged[0].ID != "stable-a" || len(merged[0].History) != 3 || merged[0].History[2].Kind != "merge" ||
		len(merged[0].History[2].Parents) != 2 {
		t.Fatalf("merge lineage lost: %+v", merged)
	}
	split := []LiveDiscovery{{ID: "new-1", Name: "One", PaperIDs: []string{"a", "b", "c"}},
		{ID: "new-2", Name: "Two", PaperIDs: []string{"d", "e", "f"}}}
	liveDiscoveryLineage(split, merged, "2026-10-01", "snapshot-observed-date-v1")
	if split[0].ID == split[1].ID || !slices.ContainsFunc(split, func(d LiveDiscovery) bool {
		return slices.ContainsFunc(d.History, func(e LiveLineageEvent) bool { return e.Kind == "split" })
	}) {
		t.Fatalf("split lineage lost: %+v", split)
	}
	future := []LiveDiscovery{{ID: "future-id", Name: "Future", PaperIDs: []string{"a", "b", "c", "d"},
		History: []LiveLineageEvent{{At: "2027-01-01"}}}}
	past := []LiveDiscovery{{ID: "new", Name: "Past", PaperIDs: []string{"a", "b", "c", "d"}}}
	liveDiscoveryLineage(past, future, "2026-09-11", "snapshot-observed-date-v1")
	if past[0].ID != "new" {
		t.Fatal("future lineage leaked into the cutoff")
	}
	if liveStem("harnesses") != liveStem("harness") || liveStem("batteries") != "battery" {
		t.Fatal("regular variants were not canonicalized")
	}
}

func TestLiveLineageDateUsesObservationAndClampsStrictHistory(t *testing.T) {
	for _, test := range []struct {
		observed, want, basis string
		strict                bool
	}{
		{"2026-09-11T06:38:49Z", "2026-09-11", "snapshot-observed-date-v1", false},
		{"2026-09-12T00:30:00+09:00", "2026-09-11", "snapshot-observed-date-v1", false},
		{"2026-10-02T06:00:00Z", "2026-09-11", "cutoff-inclusive-date-v1", true},
		{"", "2026-09-11", "cutoff-inclusive-date-v1", false},
	} {
		request := LiveAnalysisRequest{StrictAvailability: test.strict,
			Snapshot: LiveSnapshot{ObservedAt: test.observed, Periods: []LivePeriod{{EndExclusive: "2026-09-12"}}}}
		date, basis := liveLineageDate(request)
		if date != test.want || basis != test.basis {
			t.Fatalf("lineage date for %+v: %q/%q", test, date, basis)
		}
	}
}

func TestLiveAnalysisMigratesOnlyVerifiedLegacyCreationDates(t *testing.T) {
	discovery := &LiveDiscovery{ID: "semantic:a", Name: "Battery", Method: "dated-semantic-leader-v2",
		PaperIDs: []string{"paper"},
		Sampled:  true,
		History:  []LiveLineageEvent{{Kind: "created", At: "2026-09-12", Name: "Battery"}}}
	request := LiveAnalysisRequest{Snapshot: LiveSnapshot{Method: "live-signals-v5", ObservedAt: "2026-09-11T06:38:49Z",
		Periods: []LivePeriod{{Start: "2026-09-01", EndExclusive: "2026-09-12"}},
		Discovered: []LiveTopic{{ID: discovery.ID, Name: discovery.Name, PaperIDs: discovery.PaperIDs,
			Analysis: &LiveTopicAnalysis{Discovery: discovery}}}}}
	before, _ := json.Marshal(request)
	result, err := AnalyzeLive(t.Context(), nil, request)
	if err != nil {
		t.Fatal(err)
	}
	corrected := result.Topics[discovery.ID].Discovery
	expected := *discovery
	expected.History = []LiveLineageEvent{{Kind: "created", At: "2026-09-11", PreviousAt: "2026-09-12",
		DateBasis: "snapshot-observed-date-v1", Name: "Battery"}}
	if !reflect.DeepEqual(corrected, &expected) {
		t.Fatalf("migration changed scientific identity or lost original date: %+v", corrected)
	}
	after, _ := json.Marshal(request)
	if string(before) != string(after) {
		t.Fatal("migration mutated the saved input")
	}
	service := &TrendLiveService{snapshot: &request.Snapshot}
	previous, _, _ := service.previousAnalysis()
	if len(previous) != 1 || !reflect.DeepEqual(previous[0], expected) {
		t.Fatal("next full discovery did not retain the verified date correction")
	}
	for name, mutate := range map[string]func(*LiveAnalysisRequest){
		"strict":            func(r *LiveAnalysisRequest) { r.StrictAvailability = true },
		"absent time":       func(r *LiveAnalysisRequest) { r.Snapshot.ObservedAt = "" },
		"different day":     func(r *LiveAnalysisRequest) { r.Snapshot.ObservedAt = "2026-09-10T12:00:00Z" },
		"different members": func(r *LiveAnalysisRequest) { r.Snapshot.Discovered[0].PaperIDs = []string{"other"} },
		"additional event": func(r *LiveAnalysisRequest) {
			r.Snapshot.Discovered[0].Analysis.Discovery.History = append(r.Snapshot.Discovered[0].Analysis.Discovery.History,
				LiveLineageEvent{Kind: "rename", At: "2026-09-12"})
		},
		"already corrected": func(r *LiveAnalysisRequest) { r.Snapshot.Discovered[0].Analysis.Discovery = corrected },
	} {
		t.Run(name, func(t *testing.T) {
			var copied LiveAnalysisRequest
			if err := json.Unmarshal(before, &copied); err != nil {
				t.Fatal(err)
			}
			mutate(&copied)
			got, err := AnalyzeLive(t.Context(), nil, copied)
			if err != nil ||
				!reflect.DeepEqual(got.Topics[discovery.ID].Discovery, copied.Snapshot.Discovered[0].Analysis.Discovery) {
				t.Fatalf("unchecked/repeated history was rewritten: %v", err)
			}
		})
	}
}
