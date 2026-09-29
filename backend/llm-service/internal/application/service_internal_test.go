package application

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-service/internal/domain"
)

type hitCache struct{ res domain.Result }

func (c hitCache) Get(context.Context, string) (domain.Result, bool, error) {
	return c.res, true, nil
}
func (hitCache) Set(context.Context, string, domain.Result) error { return nil }
func (hitCache) Epoch(context.Context, string) int64              { return 7 }

type errGetCache struct{}

func (errGetCache) Get(context.Context, string) (domain.Result, bool, error) {
	return domain.Result{}, false, errors.New("cache down")
}
func (errGetCache) Set(context.Context, string, domain.Result) error { return nil }
func (errGetCache) Epoch(context.Context, string) int64              { return 0 }

type fakeChunkSource struct {
	gotOwner string
	chunks   []domain.StoredChunk
}

func (f *fakeChunkSource) DocumentChunks(_ context.Context, ownerID, _ string) ([]domain.StoredChunk, error) {
	f.gotOwner = ownerID
	return f.chunks, nil
}

func TestAnswerCacheHit(t *testing.T) {
	ans := &fakeAnswerer{}
	cached := domain.Result{Answer: "cached answer", Model: "m"}
	svc := buildC(hitCache{res: cached}, fakeRetriever{}, fakeRetriever{}, fakeRanker{}, ans, false)
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !res.Cached || res.Answer != "cached answer" {
		t.Fatalf("res = %+v, want the cached answer with Cached=true", res)
	}
	if ans.called {
		t.Fatal("a cache hit must not invoke the generator")
	}
}

func TestAnswerCacheGetError(t *testing.T) {
	ans := &fakeAnswerer{}
	svc := buildC(errGetCache{}, fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Cached || !ans.called {
		t.Fatalf("cache error should fall through to generation: res=%+v called=%v", res, ans.called)
	}
}

func TestChunksSharedCorpusDropsOwner(t *testing.T) {
	t.Run("shared corpus drops owner", func(t *testing.T) {
		cs := &fakeChunkSource{chunks: []domain.StoredChunk{{ID: "c0"}}}
		svc := New(fakeEmbedder{}, fakeRetriever{}, fakeRetriever{}, fakeRanker{}, &fakeAnswerer{},
			fakeCache{}, cs, nil, nil, 5, true, -5, false, Tuning{})
		if _, err := svc.Chunks(context.Background(), "owner-1", "doc-1"); err != nil {
			t.Fatalf("Chunks: %v", err)
		}
		if cs.gotOwner != "" {
			t.Fatalf("shared corpus owner = %q, want empty", cs.gotOwner)
		}
	})

	t.Run("isolation keeps owner", func(t *testing.T) {
		cs := &fakeChunkSource{chunks: []domain.StoredChunk{{ID: "c0"}}}
		svc := New(fakeEmbedder{}, fakeRetriever{}, fakeRetriever{}, fakeRanker{}, &fakeAnswerer{},
			fakeCache{}, cs, nil, nil, 5, false, -5, false, Tuning{})
		if _, err := svc.Chunks(context.Background(), "owner-1", "doc-1"); err != nil {
			t.Fatalf("Chunks: %v", err)
		}
		if cs.gotOwner != "owner-1" {
			t.Fatalf("isolation owner = %q, want owner-1", cs.gotOwner)
		}
	})
}

func TestFuseMergesProvenance(t *testing.T) {
	const dupID = "dup-chunk"
	vec := []domain.RetrievedChunk{{ID: dupID, Text: "from vector"}}
	lex := []domain.RetrievedChunk{{
		ID: dupID, DocumentID: "d1", Filename: "f.pdf", ChunkIndex: 4,
		CharStart: 5, CharEnd: 9, PageStart: 1, PageEnd: 2, SectionHeading: "H",
	}}
	out := fuse([]weightedList{{1, vec}, {1, lex}})
	if len(out) != 1 {
		t.Fatalf("want 1 fused chunk, got %d", len(out))
	}
	c := out[0]
	// Оба списка на позиции 0: 1/60 + 1/60.
	if c.Score <= 1.0/float64(rrfK) {
		t.Fatalf("score %v should accumulate across both lists", c.Score)
	}
	if c.Text != "from vector" || c.DocumentID != "d1" || c.Filename != "f.pdf" || c.ChunkIndex != 4 {
		t.Fatalf("provenance not merged: %+v", c)
	}
	if c.CharEnd != 9 || c.PageEnd != 2 || c.SectionHeading != "H" {
		t.Fatalf("span provenance not backfilled: %+v", c)
	}
}

func TestCapPerDocument(t *testing.T) {
	ranked := []domain.RetrievedChunk{
		{ID: "a1", DocumentID: "A", Score: 5},
		{ID: "a2", DocumentID: "A", Score: 4},
		{ID: "a3", DocumentID: "A", Score: 3},
		{ID: "a4", DocumentID: "A", Score: 2},
		{ID: "b1", DocumentID: "B", Score: 1},
	}
	got := capPerDocument(ranked, 3, 2)
	if len(got) != 3 {
		t.Fatalf("want 3 chunks (backfilled), got %d", len(got))
	}
	// Два из A, затем B; a3 добирается из отложенных до k=3.
	if got[0].ID != "a1" || got[1].ID != "a2" {
		t.Fatalf("top two should be A's best: %+v", got)
	}
	docA := 0
	for _, c := range got {
		if c.DocumentID == "A" {
			docA++
		}
	}
	if docA > 3 {
		t.Fatalf("doc A appears %d times, cap+backfill should bound it", docA)
	}
}

func TestCapPerDocumentDisabled(t *testing.T) {
	ranked := makeChunks(5)
	got := capPerDocument(ranked, 3, 0)
	if len(got) != 3 {
		t.Fatalf("want a plain top-3, got %d", len(got))
	}
	for i := range got {
		if got[i].ID != ranked[i].ID {
			t.Fatalf("disabled cap should preserve order: got[%d]=%s", i, got[i].ID)
		}
	}
}

type mismatchRanker struct{}

func (mismatchRanker) Rank(context.Context, string, []domain.RetrievedChunk) ([]float64, error) {
	return []float64{0.5}, nil
}

func TestRerankerLengthMismatchKeepsOrder(t *testing.T) {
	ans := &fakeAnswerer{}
	svc := build(fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{}, mismatchRanker{}, ans, false)
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(res.Sources) != 3 {
		t.Fatalf("want 3 sources kept on mismatch, got %d", len(res.Sources))
	}
}

func TestExpandSummariesStampsProvenance(t *testing.T) {
	ranked := []domain.RetrievedChunk{{
		ID: "sum-1", NodeType: nodeTypeRaptorSummary, Origin: domain.OriginDense, Score: 3,
		Members: []domain.RetrievedChunk{
			{ID: "leaf-1", Text: "leaf one", DocumentID: "d1"},
			{ID: "leaf-2", Text: "leaf two", DocumentID: "d1"},
		},
	}}
	out, expanded := expandSummaries(ranked)
	if expanded != 1 {
		t.Fatalf("expanded = %d, want 1", expanded)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 leaves, got %d", len(out))
	}
	for _, leaf := range out {
		if leaf.RaptorSummaryID != "sum-1" || leaf.Origin != domain.OriginDense || leaf.Score != 3 {
			t.Fatalf("leaf provenance not stamped: %+v", leaf)
		}
	}
}

func TestExpandSummariesPassThrough(t *testing.T) {
	ranked := makeChunks(2)
	out, expanded := expandSummaries(ranked)
	if expanded != 0 || len(out) != 2 || out[0].RaptorSummaryID != "" {
		t.Fatalf("pass-through broken: expanded=%d out=%+v", expanded, out)
	}
}

func TestSnippetTruncatesRunes(t *testing.T) {
	short := "краткий текст"
	if got := snippet("  " + short + "  "); got != short {
		t.Fatalf("short snippet = %q, want trimmed original", got)
	}
	long := make([]rune, snippetLen+50)
	for i := range long {
		long[i] = 'ы' // многобайтовая: срез по байтам разрезал бы её
	}
	got := []rune(snippet(string(long)))
	if len(got) != snippetLen+1 {
		t.Fatalf("truncated snippet len = %d runes, want %d", len(got), snippetLen+1)
	}
	if got[len(got)-1] != '…' {
		t.Fatalf("truncated snippet should end with an ellipsis: %q", string(got))
	}
}
