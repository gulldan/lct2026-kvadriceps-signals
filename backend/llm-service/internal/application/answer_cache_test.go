package application

import (
	"context"
	"testing"

	"github.com/example/llm-service/internal/domain"
)

type answerMemoryCache map[string]domain.Result

func (c answerMemoryCache) Get(_ context.Context, key string) (domain.Result, bool, error) {
	r, ok := c[key]
	return r, ok, nil
}
func (c answerMemoryCache) Set(_ context.Context, key string, r domain.Result) error {
	c[key] = r
	return nil
}
func (answerMemoryCache) Epoch(context.Context, string) int64 { return 1 }

func TestAnswerCacheSeparatesTopKAndGenerationRevision(t *testing.T) {
	cache := answerMemoryCache{}
	answerer := &fakeAnswerer{}
	s := buildC(cache, fakeRetriever{chunks: makeChunks(10)}, fakeRetriever{}, fakeRanker{score: 1}, answerer, false)
	revision := "model-a"
	s.SetCacheRevision(func(context.Context) string { return revision })
	q := domain.Query{OwnerID: "reader", Text: "turbine efficiency", TopK: 2}
	for _, tc := range []struct {
		k        int
		revision string
		cached   bool
	}{
		{2, "model-a", false}, {2, "model-a", true}, {4, "model-a", false},
		{4, "model-b", false}, {4, "model-b", true},
	} {
		q.TopK, revision = tc.k, tc.revision
		answerer.called = false
		result, err := s.Answer(t.Context(), q)
		if err != nil || result.Cached != tc.cached || len(result.Sources) != tc.k || answerer.called == tc.cached {
			t.Fatalf("cache mixed request semantics: topK=%d revision=%s result=%+v err=%v", tc.k, revision, result, err)
		}
	}
}
