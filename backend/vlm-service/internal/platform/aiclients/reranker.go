package aiclients

import (
	"context"
	"net/http"
	"strings"
)

type Reranker interface {
	Rerank(ctx context.Context, query string, docs []string) ([]float32, error)
}

func NewReranker(url, model string, c *http.Client) Reranker {
	if url == "" {
		return &stubReranker{}
	}
	return &httpReranker{url: url, model: model, httpc: httpClient(c)}
}

// httpReranker принимает и формат Triton {"scores"}, и Jina/Cohere {"results"}, чтобы подключался
// llama.cpp --reranking.
type httpReranker struct {
	url   string
	model string
	httpc *http.Client
}

func (r *httpReranker) Rerank(ctx context.Context, query string, docs []string) ([]float32, error) {
	req := map[string]any{modelField: r.model, "query": query, "documents": docs}
	var resp struct {
		Scores  []float32 `json:"scores"`
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float32 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := postJSON(ctx, r.httpc, r.url, req, &resp); err != nil {
		return nil, err
	}
	// Jina/Cohere могут переупорядочить результаты, возвращаем их к порядку docs.
	if len(resp.Scores) == 0 && len(resp.Results) > 0 {
		scores := make([]float32, len(docs))
		for _, res := range resp.Results {
			if res.Index >= 0 && res.Index < len(docs) {
				scores[res.Index] = res.RelevanceScore
			}
		}
		return scores, nil
	}
	return resp.Scores, nil
}

type stubReranker struct{}

func (r *stubReranker) Rerank(_ context.Context, query string, docs []string) ([]float32, error) {
	q := tokenSet(query)
	scores := make([]float32, len(docs))
	for i, d := range docs {
		scores[i] = jaccard(q, tokenSet(d))
	}
	return scores, nil
}

func tokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, tok := range strings.Fields(strings.ToLower(s)) {
		set[tok] = struct{}{}
	}
	return set
}

func jaccard(a, b map[string]struct{}) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for tok := range a {
		if _, ok := b[tok]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float32(inter) / float32(union)
}
