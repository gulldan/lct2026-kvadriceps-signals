package application

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLiveBurstNormalizesCoverageAndExcludesCurrentPartial(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	topic := LiveTopic{}
	for i := range periods {
		periods[i].Population = 100
		topic.Series = append(topic.Series, StudyPeriod{Period: periods[i].ID, Count: 10})
	}
	last := len(periods) - 2
	periods[last].Population = 200
	topic.Series[last].Count = 20
	topic.Series[last+1].Count = 90
	result := liveBurst(topic, periods, "same", "same", nil)
	if result.Flag == nil || *result.Flag || result.RawGrowthFlag == nil || !*result.RawGrowthFlag ||
		result.Period != "2026-08" ||
		!slices.Contains(result.PartialExcluded, "2026-09") {
		t.Fatalf("coverage/partial false alarm %+v", result)
	}
	topic.Series[last].Count = 80
	result = liveBurst(topic, periods, "same", "same", nil)
	if result.Flag == nil || !*result.Flag || *result.Score < 3 {
		t.Fatalf("clear burst missed %+v", result)
	}
	if result := liveBurst(topic, periods, "changed", "same", nil); result.Flag != nil || result.Status != "coverage_changed" {
		t.Fatal("changed coverage treated as growth")
	}
	if result := liveBurst(topic, periods, "same", "same", []LiveLimit{{Text: "arxiv window missing"}}); result.Flag != nil {
		t.Fatal("incomplete population treated as measured")
	}
}

func TestLiveIndependenceUsesResolvableIdentitiesAndExplicitLinks(t *testing.T) {
	papers := []StudyPaper{{ID: "a", Title: "A", Date: "2026-08-01", URL: "https://arxiv.org/abs/a",
		Authors: []string{"J. Smith"}},
		{ID: "b", Title: "B", Date: "2026-08-01", URL: "https://arxiv.org/abs/b", Authors: []string{"J. Smith"}},
		{ID: "unknown", Title: "C", Date: "2026-08-01", URL: "https://arxiv.org/abs/c"}}
	auth := func(author, org string) LiveAuthorship {
		return LiveAuthorship{AuthorID: "https://openalex.org/" + author, AuthorName: "J. Smith",
			OrganizationID: "https://openalex.org/" + org, SourceURL: "https://api.openalex.org/works/W1",
			ObservedAt: "2026-09-01"}
	}
	metadata := map[string]LivePaperMetadata{
		"a": {AvailableAt: "2026-08-01", SourceURL: "https://arxiv.org/abs/a", Abstract: "Code: https://github.com/team/project",
			Authorships: []LiveAuthorship{auth("A1", "I1")}},
		"b": {Authorships: []LiveAuthorship{auth("A2", "I2")}},
		"unknown": {Authorships: []LiveAuthorship{{AuthorID: "https://openalex.org/A3", OrganizationID: "https://openalex.org/I3",
			SourceURL: "https://api.openalex.org/works/W3", ObservedAt: "2027-01-01"}}},
		"https://github.com/team/project/releases/tag/v1": {Links: []LiveEvidenceLink{{Kind: "fork",
			URL: "https://github.com/fork/project", ParentURL: "https://github.com/team/project",
			SourceURL: "https://api.github.com/repos/fork/project", ObservedAt: "2026-09-01"}}},
	}
	topic := LiveTopic{PaperIDs: []string{"a", "b", "unknown"},
		External: []StudyEvent{{Date: "2026-09-01", URL: "https://github.com/team/project/releases/tag/v1"},
			{Date: "2026-09-01", URL: "https://github.com/fork/project"}}}
	result := liveIndependence(topic, papers, metadata, "2026-09-11")
	if result.DisjointOrganizationGroups != 2 || result.UnattributedPapers != 1 || result.IndependentImplementations != nil ||
		result.DependentRecords != 2 || result.DistinctEvents != 3 {
		t.Fatalf("incorrect identity or dependency claims: %+v", result)
	}
	entry := metadata["a"]
	entry.Authorships = append(entry.Authorships, auth("A1", "I2"))
	metadata["a"] = entry
	if result := liveIndependence(topic, papers, metadata, "2026-09-11"); result.DisjointOrganizationGroups != 1 {
		t.Fatal("joint organization paper did not link components")
	}
}

func TestLiveSemanticAbstractProfileAndSensitivity(t *testing.T) {
	encoder := &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: "test", Revision: "fixed",
		Vectors: [][]float64{{1, 0}, {0, 1}, {1, 0}}}}
	s := TrendLiveService{semantic: encoder}
	snapshot := LiveSnapshot{Periods: []LivePeriod{{EndExclusive: "2026-09-11"}},
		Topics: []LiveTopic{{ID: "a", PaperIDs: []string{"p1", "p2"}}, {ID: "b", PaperIDs: []string{"p3"}}},
		Papers: []StudyPaper{{ID: "p1", Title: "One"}, {ID: "p2", Title: "Two"}, {ID: "p3", Title: "Three"}}}
	if err := s.annotateSemanticMetadata(t.Context(), &snapshot, "",
		map[string]LivePaperMetadata{"p1": {Abstract: "Method mechanism", AvailableAt: "2026-08-01"},
			"p2": {Abstract: "Future text", AvailableAt: "2027-01-01"}}); err != nil {
		t.Fatal(err)
	}
	measurement := snapshot.Topics[0].Semantic
	if measurement == nil || measurement.ProfileBasis != liveProfileTitlesAndAbstracts || measurement.MissingAbstracts != 1 ||
		measurement.DistanceRange == nil || measurement.DistanceRange.Low != 0 || measurement.DistanceRange.High != 1 ||
		len(measurement.ComparedIDs) != 1 || len(measurement.NearestPaperIDs) != 1 {
		t.Fatalf("missing reproducible uncertainty %+v", measurement)
	}
	if !strings.Contains(encoder.texts[0], "Method mechanism") ||
		strings.Contains(strings.Join(encoder.texts, " "), "Future text") {
		t.Fatal("abstract version cutoff failed")
	}
}

func TestLiveAnalysisConsumesWebAndPatentLineage(t *testing.T) {
	topic := LiveTopic{Web: &LiveChannel{ObservedAt: "2026-09-01", Records: []LiveSourceRecord{
		{ID: "one", URL: "https://news.example/original", Date: "2026-08-01"},
		{ID: "two", URL: "https://news.example/copy", Date: "2026-08-02", OriginalURL: "https://news.example/original",
			OriginalBasis: "isBasedOn"},
	}}, Patents: &LiveChannel{ObservedAt: "2026-09-01", Records: []LiveSourceRecord{
		{ID: "p1", URL: "https://patents.example/one", Date: "2026-08-01", FamilyID: "application:US1",
			FamilyBasis: "explicit_application"},
		{ID: "p2", URL: "https://patents.example/two", Date: "2026-08-02", FamilyID: "application:US1",
			FamilyBasis: "explicit_application"},
	}}}
	got := liveIndependence(topic, nil, nil, "2026-09-11")
	if got.DistinctEvents != 2 || got.RecordsExamined != 4 || got.DependentRecords != 2 || got.IndependentImplementations != nil {
		t.Fatalf("source lineage lost: %+v", got)
	}
}

func TestLiveSemanticEvaluationPreservesLabelsAndMatchedSamples(t *testing.T) {
	pairs := []LiveSemanticPair{{ID: "one", Domain: "energy", Relation: "same_idea", LabelSource: "engineering fixture",
		Split: "development", ATitle: "One", BTitle: "Two", AAbstract: "Mechanism one", BAbstract: "Mechanism one"},
		{ID: "two", Domain: "physics", Relation: "different_idea", LabelSource: "engineering fixture", Split: "development",
			ATitle: "Three", BTitle: "Four"}}
	encoder := &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: "test", Revision: "fixed",
		Vectors: [][]float64{{1, 0}, {0, 1}, {1, 0}, {1, 0}, {1, 0}, {0, 1}}}}
	got, err := EvaluateLiveSemanticPairs(t.Context(), encoder, pairs, .75)
	if err != nil || got.Title.FalseNegative != 1 || got.Title.TrueNegative != 1 || got.Abstract.TruePositive != 1 ||
		got.TitleMatched.Pairs != 1 || got.Scores[1].AbstractSimilarity != nil || got.QualityStatus != liveQualityDevelopment {
		t.Fatalf("invalid evaluation %+v %v", got, err)
	}
	pairs[0].LabelSource = ""
	if _, err := EvaluateLiveSemanticPairs(t.Context(), encoder, pairs, .75); err == nil {
		t.Fatal("unlabeled data accepted as validation")
	}
}

func TestLiveEvidenceObservationIsSeparateFromHistoricalPublicationCutoff(t *testing.T) {
	if liveObservedBy("2026-09-11T25:00:00Z", "2026-09-12T10:00:00Z") || liveObservedBy("2026-09-11", "2026-09-11T10:00:00Z") {
		t.Fatal("invalid or coarse future observation time was treated as known")
	}
	auth := func(at string) LiveAuthorship {
		return LiveAuthorship{AuthorID: "https://openalex.org/A1", OrganizationID: "https://openalex.org/I1",
			SourceURL: "https://api.openalex.org/works/W1", ObservedAt: at}
	}
	request := LiveAnalysisRequest{EvidenceOnly: true, EvidenceObservedAt: "2026-09-11T10:06:00Z",
		Snapshot: LiveSnapshot{ObservedAt: "2026-09-11T10:00:00Z",
			Periods: []LivePeriod{{Start: "2026-08-01", EndExclusive: "2026-09-11"}},
			Papers: []StudyPaper{{ID: "one", Title: "One", Date: "2026-09-01", URL: "https://papers.example/one"},
				{ID: "two", Title: "Two", Date: "2026-09-02", URL: "https://papers.example/two"},
				{ID: "future", Title: "After publication cutoff", Date: "2026-09-11", URL: "https://papers.example/future"}},
			Topics: []LiveTopic{{ID: "a", PaperIDs: []string{"one", "two", "future"},
				Web: &LiveChannel{ObservedAt: "2026-09-11T10:06:00Z", Records: []LiveSourceRecord{
					{ID: "source", URL: "https://news.example/source", Date: "2026-09-10"},
					{ID: "copy", URL: "https://news.example/copy", Date: "2026-09-10",
						OriginalURL: "https://news.example/source", OriginalBasis: "isBasedOn"},
				}}}}},
		Metadata: map[string]LivePaperMetadata{"one": {Authorships: []LiveAuthorship{auth("2026-09-11T10:05:00Z")}},
			"two":    {Authorships: []LiveAuthorship{auth("2026-09-11T10:07:00Z")}},
			"future": {Authorships: []LiveAuthorship{auth("2026-09-11T10:05:00Z")}}}}
	result, err := AnalyzeLive(t.Context(), nil, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := result.Evidence["a"]
	if evidence.ObservedAt != request.EvidenceObservedAt || evidence.AttributedPapers != 1 || evidence.UnattributedPapers != 1 ||
		evidence.RecordsExamined != 4 || evidence.DependentRecords != 1 {
		t.Fatalf("live evidence lost current observations or admitted future knowledge: %+v", evidence)
	}
	request.StrictAvailability = true
	result, err = AnalyzeLive(t.Context(), nil, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence = result.Evidence["a"]
	if evidence.AttributedPapers != 0 || evidence.UnattributedPapers != 2 || evidence.DependentRecords != 0 ||
		evidence.ObservedAt != "2026-09-10T23:59:59.999999999Z" {
		t.Fatalf("historical cutoff used current affiliations or links: %+v", evidence)
	}
}
