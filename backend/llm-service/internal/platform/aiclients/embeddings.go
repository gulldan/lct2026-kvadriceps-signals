package aiclients

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"net/http"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

// NewEmbedder: dim по умолчанию 1024, как у боевой e5-large; без url — детерминированная заглушка.
func NewEmbedder(url, model string, dim int, c *http.Client) Embedder {
	if dim <= 0 {
		dim = 1024
	}
	if url == "" {
		return &stubEmbedder{dim: dim}
	}
	return &httpEmbedder{url: url, model: model, dim: dim, httpc: httpClient(c)}
}

// httpEmbedder принимает и формат Triton {"embeddings"}, и OpenAI {"data"}, чтобы llama.cpp, LM Studio
// или vLLM подключались через EMBEDDINGS_URL.
type httpEmbedder struct {
	url             string
	model           string
	dim             int
	httpc           *http.Client
	revision        string
	requireRevision bool
}

// NewVersionedEmbedder отвергает ответы незакреплённой или сменившейся модели: он строит неизменяемые
// научные снимки, и заглушка не должна публиковать доказательства.
func NewVersionedEmbedder(url, model, revision string, dim int, c *http.Client) Embedder {
	return &httpEmbedder{url: url, model: model, revision: revision, requireRevision: true, dim: dim, httpc: httpClient(c)}
}

func (e *httpEmbedder) Dim() int { return e.dim }

func (e *httpEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	req := map[string]any{modelField: e.model, "input": texts}
	var resp struct {
		Model      string      `json:"model"`
		Revision   string      `json:"revision"`
		Embeddings [][]float32 `json:"embeddings"`
		Data       []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := postJSON(ctx, e.httpc, e.url, req, &resp); err != nil {
		return nil, err
	}
	if e.requireRevision && (e.revision == "" || resp.Revision != e.revision || resp.Model != e.model) {
		return nil, errors.New("embedding response differs from the pinned model revision")
	}
	if len(resp.Embeddings) == 0 && len(resp.Data) > 0 {
		out := make([][]float32, len(resp.Data))
		for i, d := range resp.Data {
			out[i] = d.Embedding
		}
		return out, nil
	}
	return resp.Embeddings, nil
}

type stubEmbedder struct{ dim int }

func (e *stubEmbedder) Dim() int { return e.dim }

func (e *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = hashVector(t, e.dim)
	}
	return out, nil
}

func hashVector(text string, dim int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	state := h.Sum64() | 1

	vec := make([]float32, dim)
	var norm float64
	for i := range dim {
		state ^= state >> 12
		state ^= state << 25
		state ^= state >> 27
		// Старшие 53 бита -> [0,1), затем в [-1,1).
		f := float64(state>>11)/float64(uint64(1)<<53)*2 - 1
		vec[i] = float32(f)
		norm += f * f
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		norm = 1
	}
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
	return vec
}
