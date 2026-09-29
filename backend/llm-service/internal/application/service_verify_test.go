package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/example/llm-service/internal/domain"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

type fakeRetriever struct {
	chunks []domain.RetrievedChunk
	err    error
}

func (f fakeRetriever) Retrieve(
	_ context.Context, _ string, _ []float32, _ domain.RetrievalFilter, topN int,
) ([]domain.RetrievedChunk, error) {
	if f.err != nil {
		return nil, f.err
	}
	if topN >= 0 && len(f.chunks) > topN {
		return f.chunks[:topN], nil
	}
	return f.chunks, nil
}

type fakeRanker struct {
	score float64
	err   error
}

func (f fakeRanker) Rank(_ context.Context, _ string, chunks []domain.RetrievedChunk) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]float64, len(chunks))
	for i := range out {
		out[i] = f.score
	}
	return out, nil
}

type fakeAnswerer struct {
	called     bool
	gotContext string
	gotCite    bool
	reply      string
	err        error
}

func (f *fakeAnswerer) Answer(_ context.Context, contextText, _ string, cite bool) (string, string, error) {
	f.called = true
	f.gotContext = contextText
	f.gotCite = cite
	if f.err != nil {
		return "", "", f.err
	}
	r := f.reply
	if r == "" {
		r = "GENERATED-ANSWER"
	}
	return r, "fake-model", nil
}

type fakeCache struct{}

func (fakeCache) Get(context.Context, string) (domain.Result, bool, error) {
	return domain.Result{}, false, nil
}
func (fakeCache) Set(context.Context, string, domain.Result) error { return nil }
func (fakeCache) Epoch(context.Context, string) int64              { return 0 }

type recordingCache struct{ sets int }

func (c *recordingCache) Get(context.Context, string) (domain.Result, bool, error) {
	return domain.Result{}, false, nil
}
func (c *recordingCache) Set(context.Context, string, domain.Result) error {
	c.sets++
	return nil
}
func (*recordingCache) Epoch(context.Context, string) int64 { return 0 }

func makeChunks(n int) []domain.RetrievedChunk {
	const prefix = "v"
	out := make([]domain.RetrievedChunk, n)
	for i := range out {
		out[i] = domain.RetrievedChunk{
			ID:         fmt.Sprintf("%s-%d", prefix, i),
			Text:       fmt.Sprintf("passage %s %d about turbine blade efficiency", prefix, i),
			DocumentID: fmt.Sprintf("doc-%d", i),
			Filename:   "report.pdf",
			ChunkIndex: i,
		}
	}
	return out
}

func buildC(
	cache domain.Cache, vec, lex domain.Retriever, ranker domain.Ranker, ans domain.Answerer,
	rerankerActive bool,
) *RAGService {
	return New(fakeEmbedder{}, vec, lex, ranker, ans, cache, nil, nil, nil, 5, false, -5, rerankerActive, Tuning{})
}

func build(
	vec, lex domain.Retriever, ranker domain.Ranker, ans domain.Answerer,
	rerankerActive bool,
) *RAGService {
	return buildC(fakeCache{}, vec, lex, ranker, ans, rerankerActive)
}

func TestP0_1_RequestTopKReachesSources(t *testing.T) {
	ctx := context.Background()
	// Заглушка-реранкер, чтобы порог отказа не влиял на счёт.
	mk := func() *fakeAnswerer {
		return &fakeAnswerer{}
	}

	t.Run("default TopK falls back to RAG_TOP_K=5", func(t *testing.T) {
		svc := build(fakeRetriever{chunks: makeChunks(30)}, fakeRetriever{}, fakeRanker{score: 1}, mk(), false)
		res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "turbine efficiency"})
		if err != nil {
			t.Fatalf("Answer: %v", err)
		}
		if len(res.Sources) != 5 {
			t.Fatalf("default: want 5 sources, got %d", len(res.Sources))
		}
	})

	t.Run("request TopK=12 caps the final source list at 12", func(t *testing.T) {
		svc := build(fakeRetriever{chunks: makeChunks(30)}, fakeRetriever{}, fakeRanker{score: 1}, mk(), false)
		res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "turbine efficiency", TopK: 12})
		if err != nil {
			t.Fatalf("Answer: %v", err)
		}
		if len(res.Sources) != 12 {
			t.Fatalf("P0.1 would be CONFIRMED (bug) — want 12 sources, got %d", len(res.Sources))
		}
		t.Logf("P0.1 REFUTED: request TopK=12 propagated to %d sources", len(res.Sources))
	})
}

func TestP0_2_AbstainOnEmptyEvenWithStubReranker(t *testing.T) {
	ctx := context.Background()
	ans := &fakeAnswerer{}
	svc := build(
		fakeRetriever{err: errors.New("qdrant unavailable")},
		fakeRetriever{err: errors.New("opensearch unavailable")},
		fakeRanker{score: 1}, ans, false, // rerankerActive=false
	)
	res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.called {
		t.Fatal("P0.2 fail-open NOT fixed: generator was called on empty context")
	}
	if res.Answer != abstainAnswer {
		t.Fatalf("expected abstain reply on empty context, got %q", res.Answer)
	}
	t.Logf("P0.2 FIXED: empty/dead retrieval abstains even with the stub reranker")
}

// Деградированный отказ не кэшируется, иначе сбой переживёт сам себя; честно пустой поиск кэшируется.
func TestP0_2_DegradedAbstainNotCached(t *testing.T) {
	ctx := context.Background()

	degraded := &recordingCache{}
	svc := buildC(degraded,
		fakeRetriever{err: errors.New("down")}, fakeRetriever{err: errors.New("down")},
		fakeRanker{score: 1}, &fakeAnswerer{}, true)
	if _, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if degraded.sets != 0 {
		t.Fatalf("degraded abstain must not be cached, got %d Set calls", degraded.sets)
	}

	healthy := &recordingCache{}
	svc2 := buildC(healthy,
		fakeRetriever{}, fakeRetriever{}, // пусто, но без ошибки
		fakeRanker{score: 1}, &fakeAnswerer{}, true)
	if _, err := svc2.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if healthy.sets != 1 {
		t.Fatalf("healthy-empty abstain should be cached once, got %d", healthy.sets)
	}
}

func TestP0_2_AbstainWithRealRerankerOnEmpty(t *testing.T) {
	ctx := context.Background()
	ans := &fakeAnswerer{}
	svc := build(
		fakeRetriever{err: errors.New("qdrant unavailable")},
		fakeRetriever{err: errors.New("opensearch unavailable")},
		fakeRanker{score: 1}, ans, true, // rerankerActive=true
	)
	res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.called {
		t.Fatal("expected NO generation (abstain), but generator was called")
	}
	if res.Answer != abstainAnswer {
		t.Fatalf("expected abstain reply, got %q", res.Answer)
	}
	t.Logf("P0.2 closed only here: real reranker + empty retrieval → abstain, no generation")
}

func TestP0_2_AbstainOnLowScore(t *testing.T) {
	ctx := context.Background()
	ans := &fakeAnswerer{}
	svc := build(
		fakeRetriever{chunks: makeChunks(10)}, fakeRetriever{},
		fakeRanker{score: -7}, ans, true, // балл -7 ниже порога -5
	)
	res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.called {
		t.Fatal("expected abstain on low score, but generator was called")
	}
	if res.Answer != abstainAnswer {
		t.Fatalf("expected abstain reply, got %q", res.Answer)
	}
}

type errEmbedder struct{}

func (errEmbedder) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedder down")
}

// Порог 0.5 к баллам RRF (~1/60) не применяется, иначе был бы отказ.
func TestRerankerFailureDegradesToFusionOrder(t *testing.T) {
	cache := &recordingCache{}
	ans := &fakeAnswerer{}
	svc := New(fakeEmbedder{}, fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{},
		fakeRanker{err: errors.New("reranker down")}, ans, cache, nil, nil, nil,
		5, false, 0.5, true, Tuning{})
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer must degrade, not fail: %v", err)
	}
	if !ans.called || res.Answer != "GENERATED-ANSWER" {
		t.Fatalf("want generation over the fusion order, called=%v answer=%q", ans.called, res.Answer)
	}
	tr := res.Trace
	if tr == nil || !tr.Degraded || tr.DegradedReason != domain.DegradedRerankerUnavailable {
		t.Fatalf("trace = %+v, want degraded reason %q", tr, domain.DegradedRerankerUnavailable)
	}
	if tr.AbstainReason != "" {
		t.Fatalf("floor must not apply to fusion scores, got abstain %q", tr.AbstainReason)
	}
	if cache.sets != 0 {
		t.Fatalf("degraded answer must not be cached, got %d Set calls", cache.sets)
	}
}

func TestEmbedFailureContinuesLexicalOnly(t *testing.T) {
	cache := &recordingCache{}
	ans := &fakeAnswerer{}
	svc := New(errEmbedder{}, fakeRetriever{chunks: makeChunks(2)}, fakeRetriever{chunks: makeChunks(3)},
		fakeRanker{score: 1}, ans, cache, nil, nil, nil, 5, false, -5, false, Tuning{})
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer must degrade, not fail: %v", err)
	}
	if !ans.called || len(res.Sources) != 3 {
		t.Fatalf("want lexical-only generation with 3 sources, called=%v sources=%d", ans.called, len(res.Sources))
	}
	for _, src := range res.Sources {
		if src.Origin != domain.OriginBM25 {
			t.Fatalf("lexical-only source origin = %q, want %q", src.Origin, domain.OriginBM25)
		}
	}
	tr := res.Trace
	if tr == nil || tr.DegradedReason != domain.DegradedEmbedUnavailable || tr.CandidatesDense != 0 {
		t.Fatalf("trace = %+v, want embed_unavailable with no dense candidates", tr)
	}
	if cache.sets != 0 {
		t.Fatalf("degraded answer must not be cached, got %d Set calls", cache.sets)
	}
}

func TestEmbedAndLexicalFailureAbstains(t *testing.T) {
	cache := &recordingCache{}
	ans := &fakeAnswerer{}
	svc := New(errEmbedder{}, fakeRetriever{chunks: makeChunks(2)}, fakeRetriever{err: errors.New("opensearch down")},
		fakeRanker{score: 1}, ans, cache, nil, nil, nil, 5, false, -5, false, Tuning{})
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.called || res.Answer != abstainAnswer {
		t.Fatalf("want abstain without generation, called=%v answer=%q", ans.called, res.Answer)
	}
	tr := res.Trace
	if tr == nil || tr.AbstainReason != domain.AbstainNoSources ||
		tr.DegradedReason != domain.DegradedEmbedUnavailable {
		t.Fatalf("trace = %+v, want no_sources abstain under embed_unavailable", tr)
	}
	if cache.sets != 0 {
		t.Fatalf("degraded abstain must not be cached, got %d Set calls", cache.sets)
	}
}

func TestAnswerTrace(t *testing.T) {
	ans := &fakeAnswerer{reply: "Fact [S1]. Bogus [S9]."}
	// Вектор даёт v-0..v-2, лексика v-0..v-1: после слияния три, v-0 и v-1 найдены обеими ногами.
	svc := build(fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{chunks: makeChunks(2)},
		fakeRanker{score: 2}, ans, true)
	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	tr := res.Trace
	if tr == nil {
		t.Fatal("want a trace on the response")
	}
	if tr.CandidatesDense != 3 || tr.CandidatesLexical != 2 || tr.CandidatesFused != 3 || tr.CandidatesReturned != 3 {
		t.Fatalf("funnel counts = %+v", tr)
	}
	seen := make(map[string]bool, len(tr.Stages))
	for _, st := range tr.Stages {
		seen[st.Stage] = true
	}
	for _, want := range []string{"embed", "retrieve", "rerank", "generate", "total"} {
		if !seen[want] {
			t.Fatalf("missing %q stage in %+v", want, tr.Stages)
		}
	}
	if tr.TopScore != 2 || tr.ScoreFloor != -5 {
		t.Fatalf("top_score/score_floor = %v/%v, want 2/-5", tr.TopScore, tr.ScoreFloor)
	}
	if len(tr.Cited) != 1 || tr.Cited[0] != "[S1]" ||
		len(tr.UncitedRemoved) != 1 || tr.UncitedRemoved[0] != "[S9]" {
		t.Fatalf("citation report = %v / %v, want [S1] / [S9]", tr.Cited, tr.UncitedRemoved)
	}
	if tr.Degraded || tr.AbstainReason != "" {
		t.Fatalf("healthy run must not be degraded/abstained: %+v", tr)
	}
	origins := make(map[string]string, len(res.Sources))
	for _, src := range res.Sources {
		origins[src.ChunkID] = src.Origin
	}
	if origins["v-0"] != domain.OriginBoth || origins["v-2"] != domain.OriginDense {
		t.Fatalf("origins = %+v, want v-0 both / v-2 dense", origins)
	}
}

func TestP0_2_GenerateOnGoodScore(t *testing.T) {
	ctx := context.Background()
	ans := &fakeAnswerer{}
	svc := build(
		fakeRetriever{chunks: makeChunks(10)}, fakeRetriever{},
		fakeRanker{score: 3}, ans, true,
	)
	res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !ans.called || res.Answer != "GENERATED-ANSWER" {
		t.Fatalf("expected generation, called=%v answer=%q", ans.called, res.Answer)
	}
	if len(res.Sources) != 5 {
		t.Fatalf("want 5 sources, got %d", len(res.Sources))
	}
}

func TestP0_3_LabelledContextAndCiteFlag(t *testing.T) {
	ctx := context.Background()

	t.Run("plain Q&A: [Sn] labels present and cite instruction requested", func(t *testing.T) {
		ans := &fakeAnswerer{}
		svc := build(fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)
		if _, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"}); err != nil {
			t.Fatalf("Answer: %v", err)
		}
		for _, label := range []string{"[S1] ", "[S2] ", "[S3] "} {
			if !strings.Contains(ans.gotContext, label) {
				t.Fatalf("P0.3 'no labelled context' would be CONFIRMED — missing %q in:\n%s", label, ans.gotContext)
			}
		}
		if !ans.gotCite {
			t.Fatal("expected cite=true on the plain Q&A path")
		}
		t.Logf("P0.3 PARTIALLY REFUTED: labelled context [S1..S3] built and cite=true requested on chat path")
	})

	t.Run("structured-prompt path: cite suppressed", func(t *testing.T) {
		ans := &fakeAnswerer{}
		chunks := makeChunks(1)
		chunks[0].Text = strings.Repeat("Long source context. ", 20) + "The measured effect was a 15% reduction."
		svc := build(fakeRetriever{chunks: chunks}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)
		res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q", Prompt: "return JSON"})
		if err != nil {
			t.Fatalf("Answer: %v", err)
		}
		if ans.gotCite {
			t.Fatal("expected cite=false when a custom Prompt drives generation")
		}
		if len(res.Sources) != 1 || res.Sources[0].Snippet != chunks[0].Text ||
			!strings.Contains(ans.gotContext, res.Sources[0].Snippet) {
			t.Fatalf("structured answer must return the exact evidence used beyond the preview: %+v", res.Sources)
		}
		chat, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
		if err != nil || len(chat.Sources) != 1 || chat.Sources[0].Snippet != snippet(chunks[0].Text) {
			t.Fatalf("chat must retain short source previews: %+v, %v", chat.Sources, err)
		}
	})
}

func TestP0_3_ValidateCitations(t *testing.T) {
	// Три источника: S1 и S2 валидны (S1 повторяется), S9 выдуман.
	rep := validateCitations("Pressure is 16 bar [S1][S2]. See also [S9] and again [S1].", 3)
	if len(rep.Cited) != 2 {
		t.Fatalf("want 2 in-range citations, got %v", rep.Cited)
	}
	if len(rep.OutOfRange) != 1 || rep.OutOfRange[0] != 9 {
		t.Fatalf("want out-of-range [9], got %v", rep.OutOfRange)
	}
}

func TestP0_3_SanitizeStripsHallucinatedCitations(t *testing.T) {
	out, rep := sanitizeCitations("The pressure is 16 bar [S1]. Also relevant [S9].", 2)
	if strings.Contains(out, "[S9]") {
		t.Fatalf("hallucinated [S9] not stripped: %q", out)
	}
	if !strings.Contains(out, "[S1]") {
		t.Fatalf("valid [S1] was dropped: %q", out)
	}
	if len(rep.OutOfRange) != 1 {
		t.Fatalf("want 1 out-of-range, got %v", rep.OutOfRange)
	}
	if strings.Contains(out, " .") {
		t.Fatalf("leftover space before punctuation: %q", out)
	}
	t.Logf("P0.3 citation validation: sanitized %q", out)
}

func TestP0_3_AnswerStripsOutOfRangeCitations(t *testing.T) {
	ctx := context.Background()
	ans := &fakeAnswerer{reply: "Fact A [S1]. Fact B [S2]. Hallucinated [S9]."}
	svc := build(fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)
	res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if strings.Contains(res.Answer, "[S9]") {
		t.Fatalf("answer still contains hallucinated [S9]: %q", res.Answer)
	}
	if !strings.Contains(res.Answer, "[S1]") || !strings.Contains(res.Answer, "[S2]") {
		t.Fatalf("answer dropped valid citations: %q", res.Answer)
	}
}

func TestGenerationFailureReturnsExtractiveFallbackForChat(t *testing.T) {
	ctx := context.Background()
	cache := &recordingCache{}
	ans := &fakeAnswerer{err: errors.New("upstream rate limited")}
	svc := buildC(cache, fakeRetriever{chunks: makeChunks(4)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)

	res, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q"})
	if err != nil {
		t.Fatalf("Answer should degrade to extractive fallback, got error: %v", err)
	}
	if res.Model != extractiveFallbackModel {
		t.Fatalf("model = %q, want %q", res.Model, extractiveFallbackModel)
	}
	if !strings.Contains(res.Answer, "Генерация ответа сейчас недоступна") ||
		!strings.Contains(res.Answer, "[S1]") ||
		!strings.Contains(res.Answer, "passage v 0") {
		t.Fatalf("fallback answer is not source-cited extract: %q", res.Answer)
	}
	if len(res.Sources) != 4 {
		t.Fatalf("fallback must return the retrieved sources, got %d", len(res.Sources))
	}
	if cache.sets != 0 {
		t.Fatalf("extractive fallback must not be cached, got %d Set calls", cache.sets)
	}
}

// Структурным вызовам нужен JSON: им отдаём ошибку генерации, а не текстовую заглушку.
func TestGenerationFailurePropagatesForStructuredPrompt(t *testing.T) {
	ctx := context.Background()
	ans := &fakeAnswerer{err: errors.New("upstream rate limited")}
	svc := build(fakeRetriever{chunks: makeChunks(2)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)

	if _, err := svc.Answer(ctx, domain.Query{OwnerID: "u1", Text: "q", Prompt: "return JSON"}); err == nil {
		t.Fatal("structured prompt generation error must propagate")
	}
}
