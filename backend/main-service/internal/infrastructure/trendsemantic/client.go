package trendsemantic

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/main-service/internal/application"
)

type Client struct {
	URL, Model string
	HTTP       *http.Client
}

func New(url, model string) *Client {
	return &Client{URL: url, Model: model, HTTP: &http.Client{Timeout: 25 * time.Second}}
}

// Encode сохраняет порядок входа и ревизию модели провайдера.
func (c *Client) Encode(ctx context.Context, texts []string) (application.LiveEmbeddingBatch, error) {
	result := application.LiveEmbeddingBatch{}
	// Пачки по 4 держат общий GPU отзывчивым; профиль — не больше двенадцати коротких названий.
	for start := 0; start < len(texts); start += 4 {
		batch, err := c.encodeBatch(ctx, texts[start:min(start+4, len(texts))])
		if err != nil {
			return application.LiveEmbeddingBatch{}, err
		}
		if start == 0 {
			result.Model, result.Revision = batch.Model, batch.Revision
		}
		if batch.Model != result.Model || batch.Revision != result.Revision {
			return application.LiveEmbeddingBatch{}, errors.New("semantic: model changed between batches")
		}
		result.Vectors = append(result.Vectors, batch.Vectors...)
	}
	return result, nil
}

func (c *Client) encodeBatch(ctx context.Context, texts []string) (application.LiveEmbeddingBatch, error) {
	result := application.LiveEmbeddingBatch{}
	body, err := json.Marshal(map[string]any{"model": c.Model, "input": texts})
	if err != nil {
		return result, fmt.Errorf("semantic: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("semantic: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return result, fmt.Errorf("semantic: model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("semantic: model status %d", resp.StatusCode)
	}
	const limit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return result, fmt.Errorf("semantic: read: %w", err)
	}
	if len(raw) > limit {
		return result, errors.New("semantic: oversized response")
	}
	var payload struct {
		Model    string `json:"model"`
		Revision string `json:"revision"`
		Data     []struct {
			Index  int       `json:"index"`
			Vector []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return result, fmt.Errorf("semantic: response: %w", err)
	}
	if payload.Model != c.Model || payload.Revision == "" || len(payload.Data) != len(texts) {
		return result, errors.New("semantic: incompatible model response")
	}
	result.Model, result.Revision = payload.Model, payload.Revision
	result.Vectors = make([][]float64, len(texts))
	for _, row := range payload.Data {
		if row.Index < 0 || row.Index >= len(texts) || result.Vectors[row.Index] != nil || len(row.Vector) == 0 {
			return application.LiveEmbeddingBatch{}, errors.New("semantic: invalid response index")
		}
		result.Vectors[row.Index] = row.Vector
	}
	return result, nil
}
