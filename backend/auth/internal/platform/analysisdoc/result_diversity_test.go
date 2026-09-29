package analysisdoc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/example/auth/internal/platform/indexdoc"
	"github.com/example/auth/internal/platform/sourcedoc"
)

type diversityObjects map[string]string

func TestRepresentativePrefersVerifiedBodyAndPreservesLegacyMethod(t *testing.T) {
	records := map[string]Record{
		"a": {ID: "a", Representation: "metadata_or_text", Source: sourcedoc.Source{Provider: "web", TextStatus: "metadata_only"}},
		"z": {ID: "z", Representation: "metadata_or_text", Source: sourcedoc.Source{Provider: "web", TextStatus: "article_text"}},
	}
	work := Work{DocumentIDs: []string{"z", "a"}}
	for _, test := range []struct {
		body bool
		want string
	}{{true, "z"}, {false, "a"}} {
		got, err := representative(work, records, test.body)
		if err != nil || got.ID != test.want {
			t.Fatalf("body=%v: got %q, want %q: %v", test.body, got.ID, test.want, err)
		}
	}
	if matchesDiversityMethod(diversityVersion, &Diversity{Version: verifiedBodyDiversityVersion}) ||
		matchesDiversityMethod(verifiedBodyDiversityVersion, &Diversity{Version: diversityVersion}) ||
		matchesDiversityMethod(verifiedBodyDiversityVersion, nil) {
		t.Fatal("a different representation method was accepted")
	}
}

func (objects diversityObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if data, exists := objects[key]; exists {
		return io.NopCloser(strings.NewReader(data)), nil
	}
	return nil, errors.New("missing pinned snapshot")
}

func TestDiversityRecomputesSourceVectorsAndRejectsForgedOrder(t *testing.T) {
	input := Input{Model: &indexdoc.Model{Name: "fixed-model", Revision: "immutable-revision", Dimension: 3}}
	result := Result{Ranking: Ranking{OrderedIDs: []string{"a", "c"},
		Diversity: &Diversity{Version: diversityVersion, Lambda: .75, DuplicateThreshold: .92,
			InputSHA256: strings.Repeat("a", 64), SuppressedIDs: []string{"b"}}}}
	store := diversityObjects{}
	for _, id := range []string{"a", "b", "c"} {
		vector := []float64{1, 0, 0}
		if id == "c" {
			vector = []float64{0, 1, 0}
		}
		candidate := Candidate{ID: id, CompleteForBrief: true, Relevance: Relevance{Relevant: true},
			Brief:      Brief{Problem: &BriefField{Text: "p"}, Advantage: &BriefField{Text: "a"}},
			Assessment: jsontext.Value(`{"eligible":true,"order":[1,0.1,2,2]}`)}
		for _, suffix := range []string{"1", "2"} {
			work, doc := "work-"+id+suffix, "doc-"+id+suffix
			snapshot := map[string]any{"document_id": doc, "snapshot_id": "snapshot-" + doc, "model": input.Model,
				"chunks": []map[string]any{{"vector": vector}}}
			data, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			store[doc] = string(data)
			record := Record{ID: doc, Representation: "abstract", Index: indexdoc.Manifest{DocumentID: doc,
				SnapshotID: "snapshot-" + doc, Model: *input.Model, ChunkCount: 1,
				Data: indexdoc.Object{Key: doc, SHA256: indexdoc.Hash(data)}}}
			input.Records = append(input.Records, record)
			result.Works = append(result.Works, Work{ID: work, DocumentIDs: []string{doc}})
			candidate.WorkIDs = append(candidate.WorkIDs, work)
		}
		result.Candidates = append(result.Candidates, candidate)
	}
	if err := verifyDiversity(t.Context(), store, input, result, 0); err != nil {
		t.Fatal("verified source-vector order rejected", err)
	}
	var snapshotBytes int64
	for _, data := range store {
		snapshotBytes += int64(len(data))
	}
	if err := verifyDiversity(t.Context(), store, input, result, MaxAnalysisArtifactBytes-snapshotBytes); err != nil {
		t.Fatal("exact cumulative artifact budget rejected", err)
	}
	if verifyDiversity(t.Context(), store, input, result, MaxAnalysisArtifactBytes-snapshotBytes+1) == nil {
		t.Fatal("cumulative artifact budget overflow accepted")
	}
	result.Ranking.OrderedIDs = []string{"a", "b", "c"}
	result.Ranking.Diversity.SuppressedIDs = []string{}
	if verifyDiversity(t.Context(), store, input, result, 0) == nil {
		t.Fatal("semantic duplicate occupied a ranked position")
	}
	result.Ranking.OrderedIDs = []string{"a", "c"}
	result.Ranking.Diversity.SuppressedIDs = []string{"b"}
	store["doc-c1"] = strings.ReplaceAll(store["doc-c1"], "[0,1,0]", "[1,0,0]")
	if verifyDiversity(t.Context(), store, input, result, 0) == nil {
		t.Fatal("changed vectors accepted under the old source hash")
	}
	delete(store, "doc-c1")
	if verifyDiversity(t.Context(), store, input, result, 0) == nil {
		t.Fatal("unavailable source became a valid ranking")
	}
}

func TestDiversityStableQuantizedOrderingAndZeroSeven(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f", "g"}
	pool, centers := make([]string, 0, len(ids)), map[string][]float64{}
	for _, id := range ids {
		vector := make([]float64, 7)
		vector[len(pool)] = 1
		pool = append(pool, id)
		centers[id] = vector
	}
	got, suppressed := diversify(pool, centers)
	if !slices.Equal(got, pool) || len(suppressed) != 0 {
		t.Fatal("seven topics were padded or reordered", got)
	}
	got, suppressed = diversify(nil, nil)
	if len(got) != 0 || len(suppressed) != 0 {
		t.Fatal("empty input was padded")
	}
}
