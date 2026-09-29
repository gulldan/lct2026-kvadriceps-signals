package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
)

type semanticTestEncoder struct {
	batch LiveEmbeddingBatch
	err   error
	texts []string
}

func (e *semanticTestEncoder) Encode(_ context.Context, texts []string) (LiveEmbeddingBatch, error) {
	e.texts = slices.Clone(texts)
	return e.batch, e.err
}

func TestLiveSemanticComparisonAndQuery(t *testing.T) {
	encoder := &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: "local", Revision: "pinned",
		Vectors: [][]float64{{3, 0}, {4, 0}, {0, 5}, {6, 0}}}}
	s := &TrendLiveService{semantic: encoder}
	snapshot := LiveSnapshot{Topics: []LiveTopic{{ID: "a", Name: "A", PaperIDs: []string{"p1"}},
		{ID: "b", Name: "B", PaperIDs: []string{"p2"}}, {ID: "c", Name: "C", PaperIDs: []string{"p3"}}},
		Papers: []StudyPaper{{ID: "p1", Title: "One"}, {ID: "p2", Title: "Two"}, {ID: "p3", Title: "Three"}}}
	if err := s.annotateSemanticMetadata(t.Context(), &snapshot, "query", nil); err != nil {
		t.Fatal(err)
	}
	got := snapshot.Topics[0].Semantic
	if got == nil || got.NearestID != "b" || got.Distance != 0 || got.Compared != 2 || got.QuerySimilarity == nil ||
		*got.QuerySimilarity != 1 || got.Revision != "pinned" {
		t.Fatalf("bad relative comparison: %+v", got)
	}
	if snapshot.Topics[2].Semantic.Distance != 1 || encoder.texts[0] != "One" || encoder.texts[3] != "query" {
		t.Fatal("unrelated concept or query profile is wrong")
	}
	if !slices.Equal(got.ComparedIDs, []string{"b", "c"}) || !slices.Equal(got.ComparedNames, []string{"B", "C"}) {
		t.Fatal("comparison names do not match the measured topic IDs")
	}
}

func TestLiveSemanticNeverInventsMissingMeasurement(t *testing.T) {
	for _, test := range []struct {
		name    string
		vectors [][]float64
		err     error
	}{
		{"unavailable", nil, errors.New("down")}, {"missing", [][]float64{{1, 0}}, nil},
		{"zero", [][]float64{{0, 0}, {1, 0}}, nil}, {"dimension", [][]float64{{1}, {1, 0}}, nil},
		{"nan", [][]float64{{math.NaN(), 0}, {1, 0}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoder := &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: "local", Revision: "pinned", Vectors: test.vectors},
				err: test.err}
			s := &TrendLiveService{semantic: encoder}
			snap := LiveSnapshot{Topics: []LiveTopic{{ID: "a", PaperIDs: []string{"a"}, Semantic: &LiveSemantic{Distance: .9}},
				{ID: "b", PaperIDs: []string{"b"}}}, Papers: []StudyPaper{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}}
			if err := s.enrichAnalysis(t.Context(), &snap, "", false); err == nil {
				t.Fatal("invalid model response was accepted")
			}
			if snap.Topics[0].Semantic != nil || snap.Topics[1].Semantic != nil {
				t.Fatal("failure retained a misleading score")
			}
		})
	}
}

func TestLiveTitleSamplingIsDatedDeterministicAndDeduplicated(t *testing.T) {
	topic := LiveTopic{PaperIDs: []string{}}
	papers := make([]StudyPaper, 0, 27)
	for i := range 25 {
		id := fmt.Sprintf("%02d", i)
		topic.PaperIDs = append(topic.PaperIDs, id)
		papers = append(papers, StudyPaper{ID: id, Title: "Paper " + id, Date: id})
	}
	papers = append(papers, papers[0], StudyPaper{ID: "other", Title: "Unrelated"})
	_, first := liveTitleProfile(topic, papers)
	slices.Reverse(papers)
	_, second := liveTitleProfile(topic, papers)
	if len(first) != 12 || first[0] != "00" || first[11] != "24" || !slices.Equal(first, second) {
		t.Fatalf("biased or unstable sampling: %v / %v", first, second)
	}
}
