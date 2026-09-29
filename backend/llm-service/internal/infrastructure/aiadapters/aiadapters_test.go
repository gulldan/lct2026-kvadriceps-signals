package aiadapters_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/infrastructure/aiadapters"
	"github.com/example/llm-service/internal/platform/aiclients"
)

var errBackend = errors.New("backend down")

type fakeEmbedder struct {
	got  []string
	vecs [][]float32
	err  error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.got = texts
	if f.err != nil {
		return nil, f.err
	}
	return f.vecs, nil
}

func (*fakeEmbedder) Dim() int { return 3 }

type fakeReranker struct {
	gotQuery string
	gotDocs  []string
	scores   []float32
	err      error
}

func (f *fakeReranker) Rerank(_ context.Context, query string, docs []string) ([]float32, error) {
	f.gotQuery, f.gotDocs = query, docs
	if f.err != nil {
		return nil, f.err
	}
	return f.scores, nil
}

type fakeGenerator struct {
	got  aiclients.GenRequest
	reqs []aiclients.GenRequest
	resp aiclients.GenResponse
	then []aiclients.GenResponse
	err  error
	wait time.Duration
}

func (f *fakeGenerator) Generate(ctx context.Context, req aiclients.GenRequest) (aiclients.GenResponse, error) {
	f.got = req
	f.reqs = append(f.reqs, req)
	if f.wait > 0 {
		timer := time.NewTimer(f.wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return aiclients.GenResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	if f.err != nil {
		return aiclients.GenResponse{}, f.err
	}
	if n := len(f.reqs) - 2; n >= 0 && n < len(f.then) {
		return f.then[n], nil
	}
	return f.resp, nil
}

func (*fakeGenerator) Stream(context.Context, aiclients.GenRequest, func(string) error) (aiclients.GenResponse, error) {
	return aiclients.GenResponse{}, nil
}

func TestEmbedderEmbed(t *testing.T) {
	fake := &fakeEmbedder{vecs: [][]float32{{0.1, 0.2, 0.3}}}
	got, err := aiadapters.NewEmbedder(fake).Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(fake.got) != 1 || fake.got[0] != "hello" {
		t.Fatalf("client got batch %v, want [hello]", fake.got)
	}
	if len(got) != 3 || got[0] != 0.1 {
		t.Fatalf("vector = %v, want [0.1 0.2 0.3]", got)
	}
}

func TestEmbedderEmbedClientError(t *testing.T) {
	_, err := aiadapters.NewEmbedder(&fakeEmbedder{err: errBackend}).Embed(context.Background(), "x")
	if !errors.Is(err, errBackend) {
		t.Fatalf("err = %v, want wrapped errBackend", err)
	}
}

func TestEmbedderEmbedEmpty(t *testing.T) {
	_, err := aiadapters.NewEmbedder(&fakeEmbedder{vecs: [][]float32{}}).Embed(context.Background(), "x")
	if err == nil {
		t.Fatal("expected an error when no vector is returned")
	}
}

func TestRerankerRank(t *testing.T) {
	fake := &fakeReranker{scores: []float32{0.5, 0.25}}
	chunks := []domain.RetrievedChunk{{Text: "alpha"}, {Text: "beta"}}
	got, err := aiadapters.NewReranker(fake).Rank(context.Background(), "q", chunks)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if fake.gotQuery != "q" {
		t.Fatalf("query = %q, want q", fake.gotQuery)
	}
	wantDocs := []string{"alpha", "beta"}
	if len(fake.gotDocs) != len(wantDocs) {
		t.Fatalf("docs = %v, want %v", fake.gotDocs, wantDocs)
	}
	for i := range wantDocs {
		if fake.gotDocs[i] != wantDocs[i] {
			t.Fatalf("docs[%d] = %q, want %q", i, fake.gotDocs[i], wantDocs[i])
		}
	}
	if len(got) != 2 || got[0] != 0.5 || got[1] != 0.25 {
		t.Fatalf("scores = %v, want [0.5 0.25]", got)
	}
}

func TestRerankerRankClientError(t *testing.T) {
	_, err := aiadapters.NewReranker(&fakeReranker{err: errBackend}).
		Rank(context.Background(), "q", []domain.RetrievedChunk{{Text: "a"}})
	if !errors.Is(err, errBackend) {
		t.Fatalf("err = %v, want wrapped errBackend", err)
	}
}

func TestGeneratorAnswerCite(t *testing.T) {
	fake := &fakeGenerator{resp: aiclients.GenResponse{Text: "answer", Model: "m1"}}
	answer, model, err := aiadapters.NewGenerator(fake).Answer(context.Background(), "ctx", "q", true)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if answer != "answer" || model != "m1" {
		t.Fatalf("got (%q, %q), want (answer, m1)", answer, model)
	}
	if !fake.got.Cite || fake.got.Context != "ctx" || fake.got.Query != "q" {
		t.Fatalf("request = %+v, want cite/ctx/q passed through", fake.got)
	}
	if fake.got.MaxTokens != 1024 || fake.got.Temperature != 0.0 {
		t.Fatalf("cite budget = (%d, %v), want (1024, 0.0)", fake.got.MaxTokens, fake.got.Temperature)
	}
}

func TestGeneratorAnswerNoCite(t *testing.T) {
	fake := &fakeGenerator{resp: aiclients.GenResponse{Text: "json", Model: "m2"}}
	if _, _, err := aiadapters.NewGenerator(fake).Answer(context.Background(), "", "q", false); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if fake.got.Cite {
		t.Fatal("cite should be false on the structured-prompt path")
	}
	if fake.got.MaxTokens != 12288 || fake.got.Temperature != 0.2 {
		t.Fatalf("no-cite budget = (%d, %v), want (12288, 0.2)", fake.got.MaxTokens, fake.got.Temperature)
	}
}

func TestGeneratorAnswerClientError(t *testing.T) {
	_, _, err := aiadapters.NewGenerator(&fakeGenerator{err: errBackend}).
		Answer(context.Background(), "ctx", "q", true)
	if !errors.Is(err, errBackend) {
		t.Fatalf("err = %v, want wrapped errBackend", err)
	}
}

func TestGeneratorAnswerTimeout(t *testing.T) {
	start := time.Now()
	_, _, err := aiadapters.NewGeneratorWithTimeout(
		&fakeGenerator{wait: time.Second},
		20*time.Millisecond,
	).Answer(context.Background(), "ctx", "q", true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want wrapped context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("timeout took %v, want prompt cancellation", elapsed)
	}
}
