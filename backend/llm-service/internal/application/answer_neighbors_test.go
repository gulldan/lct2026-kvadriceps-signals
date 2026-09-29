package application

import (
	"context"
	"strings"
	"testing"

	"github.com/example/llm-service/internal/domain"
)

type neighborChunkStore map[string][]domain.StoredChunk

func (s neighborChunkStore) DocumentChunks(_ context.Context, _, documentID string) ([]domain.StoredChunk, error) {
	return s[documentID], nil
}

func TestAnswerIncludesNeighboringFindingsWithTheirOwnCitations(t *testing.T) {
	const title = "TeleAntiFraud 2.0.pdf"
	chunks := []domain.StoredChunk{
		{ID: "intro", Index: 0, Text: "TeleAntiFraud 2.0 introduces a refreshable benchmark.",
			IndexSnapshot: "active", Filename: title},
		{ID: "text", Index: 1, Text: "Text classifiers fall to 0.65–0.68 Macro-F1 on near-domain negatives.",
			IndexSnapshot: "active", Filename: title, SectionHeading: "Abstract", PageStart: 1, PageEnd: 1, CharStart: 100, CharEnd: 190},
		{ID: "audio", Index: 2, Text: "Audio and ASR+LLM evaluations reveal class-prior shortcuts and prediction collapse.",
			IndexSnapshot: "active", Filename: title, SectionHeading: "Abstract", PageStart: 1, PageEnd: 1, CharStart: 180, CharEnd: 270},
		{ID: "old", Index: 1, Text: "STALE results from another snapshot", IndexSnapshot: "old"},
		{ID: "distant", Index: 20, Text: "DISTANT unrelated appendix", IndexSnapshot: "active"},
	}
	answerer := &fakeAnswerer{reply: "Текстовые результаты [S2]. Сбои аудио-оценки [S3]."}
	svc := New(fakeEmbedder{}, fakeRetriever{chunks: []domain.RetrievedChunk{{
		ID: "intro", DocumentID: "paper", Text: chunks[0].Text, Filename: title, IndexSnapshot: "active",
	}}}, fakeRetriever{}, fakeRanker{score: 1}, answerer, fakeCache{}, &fakeChunkSource{chunks: chunks}, nil, nil,
		1, false, 0.5, true, Tuning{})
	result, err := svc.Answer(t.Context(), domain.Query{OwnerID: "alice", Text: "Какие ограничения выявил TeleAntiFraud 2.0?"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{chunks[1].Text, chunks[2].Text, "Document: " + title, "Section: Abstract"} {
		if !strings.Contains(answerer.gotContext, want) {
			t.Errorf("generator lost finding or experimental context %q", want)
		}
	}
	if strings.Contains(answerer.gotContext, "STALE") || strings.Contains(answerer.gotContext, "DISTANT") {
		t.Fatal("neighbor expansion crossed the active snapshot or bounded evidence window")
	}
	if len(result.Sources) != 3 || result.Sources[1].ChunkID != "text" || result.Sources[2].ChunkID != "audio" ||
		result.Sources[2].IndexSnapshot != "active" || result.Sources[2].CharStart != 180 || result.Sources[2].PageStart != 1 {
		t.Fatalf("citation provenance lost: %+v", result.Sources)
	}
	if result.Trace.CandidatesReturned != len(result.Sources) {
		t.Fatalf("trace hides neighboring context: %+v", result.Trace)
	}
}

func TestEvidenceNeighborsCannotReadForbiddenDocument(t *testing.T) {
	store := neighborChunkStore{
		"private": {{ID: "hidden", Index: 0}, {ID: "secret", Index: 1, Text: "SECRET neighboring result"}},
		"public": {{ID: "visible", Index: 0, IndexSnapshot: "ready"},
			{ID: "detail", Index: 1, Text: "Visible neighboring result", IndexSnapshot: "ready"}},
	}
	svc := &RAGService{chunks: store}
	svc.EnableSourceReads(policyFixture(), true)
	got := svc.withEvidenceNeighbors(t.Context(), "bob", []domain.RetrievedChunk{
		{ID: "hidden", DocumentID: "private"}, {ID: "visible", DocumentID: "public", IndexSnapshot: "ready"},
	})
	if len(got) != 3 || got[2].ID != "detail" {
		t.Fatalf("forbidden neighbor was loaded or public context lost: %+v", got)
	}
	for _, chunk := range got {
		if strings.Contains(chunk.Text, "SECRET") {
			t.Fatal("neighbor expansion bypassed source access")
		}
	}
}
