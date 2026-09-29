package application

import (
	"context"
	"encoding/json/v2"
	"github.com/example/main-service/internal/platform/contracts"
	"math"
	"testing"

	"github.com/example/main-service/internal/domain"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

func TestStudyCountsAreReproducibleAndBounded(t *testing.T) {
	corpus, err := studyCatalog()
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := studyEvidence()
	topics := studyTopics(corpus.Papers, true, evidence.Events)
	graph := topics[0]
	prior, recent := graph.Growth.Prior, graph.Growth.Recent
	if prior.Count != 4 || prior.Population != 104 || recent.Count != 29 || recent.Population != 318 || len(graph.PaperIDs) != 33 {
		t.Fatalf("wrong screened population: %+v", graph.Growth)
	}
	want := (29.0/318 - 4.0/104) * 100
	if graph.Growth.DeltaPP == nil || math.Abs(*graph.Growth.DeltaPP-want) > 1e-9 ||
		graph.Stage != "emerging_signal" || len(graph.EvidenceIDs) != 5 {
		t.Fatalf("incorrect screening signal: %+v", graph)
	}
	agentic := topics[3]
	last := graph.Series[5]
	if graph.Groups != 30 || last.NewGroups != 26 || last.RetainedGroups != 1 || graph.GrowthStart != "2024" ||
		graph.Signals.Spread != "broad" || graph.Signals.Concentration != "low" ||
		graph.Signals.Application != "releases" || agentic.Stage != "emerging_signal" ||
		agentic.EarliestExternal != "2023-05-12" || topics[2].Stage != "established" ||
		topics[1].Stage != "emerging_signal" || topics[4].Signals.Momentum != "down" {
		t.Fatalf("incorrect screened signals: graph=%+v agentic=%+v", graph.Signals, agentic.Signals)
	}
	for _, topic := range studyTopics(corpus.Papers[:len(corpus.Papers)-1], false, evidence.Events) {
		if topic.Momentum != "unknown" || topic.Stage != "unassessed" || topic.Growth.DeltaPP != nil ||
			topic.GrowthStart != "" || topic.TopGroupShare != nil || topic.Signals.Novelty != "unknown" {
			t.Fatal("missing source became a growth/stage claim")
		}
		for _, period := range topic.Series {
			if period.Share != nil {
				t.Fatal("partial population has a comparable share")
			}
		}
	}
	zero := studyGrowth(StudyPeriod{Share: new(0.0)}, StudyPeriod{Count: 1, Population: 10, Share: new(0.1)}, true)
	if zero.ShareRatio != nil || zero.CountRatio != nil || zero.DeltaPP == nil {
		t.Fatal("zero baseline became an infinite ratio")
	}
}

type studyDocuments struct {
	domain.DocumentCatalog
	documents []*commonv1.Document
}

func (d studyDocuments) ListDocuments(_ context.Context, _ string, _ ...contracts.DocumentScan) ([]*commonv1.Document, error) {
	return d.documents, nil
}

type studyIndexes struct{ states indexdoc.States }

func (s studyIndexes) ReadIndexes(context.Context, indexdoc.Read) (indexdoc.States, error) {
	return s.states, nil
}

func TestStudyReadsOnlyExactReadyAccessibleSourcesAndDeduplicates(t *testing.T) {
	corpus, _ := studyCatalog()
	paper := corpus.Papers[0]
	source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: paper.CanonicalID,
		CanonicalID: paper.CanonicalID, URL: paper.URL,
		Publication: &sourcedoc.Date{Value: paper.Date, Precision: "month", Basis: "ACL volume date"},
		ContentHash: paper.ContentHash, HashAlgorithm: "sha256", AccessScope: "public", Revision: "sha256:" + paper.ContentHash}
	// Настоящие метаданные импорта, включая их метки времени.
	if err := json.Unmarshal([]byte(studySourceMetadata(paper, corpus.ObservedAt)), &source); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(source)
	docs := []*commonv1.Document{{Id: "a", SourceMetadata: string(raw)}, {Id: "b", SourceMetadata: string(raw)}}
	manifest := func(id string) *indexdoc.Manifest {
		return &indexdoc.Manifest{DocumentID: id, SnapshotID: id + "-ready", RawHash: paper.ContentHash,
			SourceRevision: source.Revision}
	}
	states := indexdoc.States{Documents: []indexdoc.State{
		{DocumentID: "a", Allowed: true, Active: manifest("a")},
		{DocumentID: "b", Allowed: true, Active: manifest("b")},
	}}
	service := &IngestionService{docs: studyDocuments{documents: docs}, sourceIndexes: studyIndexes{states}, sharedCorpus: true}
	view, err := service.TrendStudy(t.Context(), "reader")
	if err != nil || view.AvailablePapers != 1 || view.Papers[0].DocumentID != "a" ||
		view.Coverage != "partial" {
		t.Fatalf("duplicates or exact source selection: %+v %v", view, err)
	}
	states.Documents[0].Allowed = false
	states.Documents[1].Active.RawHash = "different"
	service.sourceIndexes = studyIndexes{states}
	view, err = service.TrendStudy(t.Context(), "reader")
	if err != nil || view.AvailablePapers != 0 {
		t.Fatal("revoked or changed source contributed to counts", err)
	}
}

func studySourceMetadata(paper StudyPaper, observed string) string {
	raw, _ := json.Marshal(map[string]any{"version": 1, "source_kind": "paper", "provider": "manual",
		"native_id": paper.CanonicalID, "canonical_id": paper.CanonicalID, "url": paper.URL,
		"publication": map[string]string{"value": paper.Date, "precision": "month", "basis": "ACL volume date"},
		"observed_at": observed, "available_at": observed, "available_basis": "snapshot observation",
		"revision": "sha256:" + paper.ContentHash, "content_hash": paper.ContentHash, "hash_algorithm": "sha256",
		"access_scope": "public"})
	return string(raw)
}
