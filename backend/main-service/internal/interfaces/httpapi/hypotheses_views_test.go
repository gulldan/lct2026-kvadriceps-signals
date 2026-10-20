package httpapi

import (
	"encoding/json/v2"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestHypothesisEvidenceSurvivesFullViewUpdate(t *testing.T) {
	t.Parallel()
	existing := &commonv1.HypothesisEvidence{
		Id: "evidence-1", DocumentId: new("document-1"), ChunkId: "chunk-1", Filename: "study.pdf",
		Snippet: "Прочность выросла на 15 %.", Stance: "supports", Score: new(0.8), Ord: 2,
		Relation: "Подтверждает увеличение прочности.", PageStart: 4, PageEnd: 5,
		SectionHeading: "Результаты испытаний", Origin: "retrieval",
	}
	view := newHypothesisView(&commonv1.Hypothesis{
		Id: "hypothesis-1", Version: 7, Title: "Исходный тезис", Statement: "Проверяемое утверждение",
		Evidence: []*commonv1.HypothesisEvidence{existing},
	})
	view.Title = "Уточнённый тезис"
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var input hypothesisInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	var added evidenceInput
	if err := json.Unmarshal([]byte(`{"document_id":"","snippet":"Новый источник для проверки.",`+
		`"stance":"context","relation":"Уточняет условия испытаний.","page_start":8,"page_end":9,`+
		`"section_heading":"Методы","origin":"manual"}`), &added); err != nil {
		t.Fatal(err)
	}
	input.Evidence = append(input.Evidence, added)
	got := input.toProto(view.ID)
	if got.GetId() != view.ID || got.GetVersion() != view.Version || got.GetTitle() != view.Title || len(got.Evidence) != 2 {
		t.Fatalf("full-object edit lost hypothesis fields or evidence: %+v", got)
	}
	if !proto.Equal(got.Evidence[0], existing) {
		t.Fatalf("existing evidence lost its identity or provenance: got %v, want %v", got.Evidence[0], existing)
	}
	wantNew := &commonv1.HypothesisEvidence{Snippet: "Новый источник для проверки.", Stance: "context",
		Relation: "Уточняет условия испытаний.", PageStart: 8, PageEnd: 9, SectionHeading: "Методы", Origin: "manual"}
	if !proto.Equal(got.Evidence[1], wantNew) {
		t.Fatalf("new evidence lost provenance or acquired an ID: got %v, want %v", got.Evidence[1], wantNew)
	}
}
