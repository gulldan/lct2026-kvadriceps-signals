// Package vectorstore ходит в Qdrant по REST, чтобы не тянуть тяжёлый gRPC-клиент.
package vectorstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/rag-mvp/pkg/jsonx"
)

const (
	qWithPayload = "with_payload"
	qValue       = "value"
	qMust        = "must"
	qAny         = "any"
	qOwnerID     = "owner_id"

	qKey       = "key"
	qDirection = "direction"
	qAsc       = "asc"
	qMatch     = "match"
)

type Qdrant struct {
	base       string
	collection string
	httpc      *http.Client
}

func NewQdrant(baseURL, collection string) *Qdrant {
	return &Qdrant{base: baseURL, collection: collection, httpc: &http.Client{Timeout: 30 * time.Second}}
}

type Point struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type SearchHit struct {
	ID      string         `json:"id"`
	Score   float32        `json:"score"`
	Payload map[string]any `json:"payload"`
}

func (q *Qdrant) EnsureCollection(ctx context.Context, dim int) error {
	if dim < 1 || dim > 65536 {
		return fmt.Errorf("invalid vector dimension: %d", dim)
	}
	if err := q.ensureCollectionSchema(ctx, dim); err != nil {
		return err
	}
	// Индексы payload — часть контракта, а не оптимизация: без них каждый фильтр owner_id/document_id
	// сканирует payload с диска.
	if err := q.ensurePayloadIndex(ctx, "owner_id", keywordPayload); err != nil {
		return err
	}
	if err := q.ensurePayloadIndex(ctx, "document_id", keywordPayload); err != nil {
		return err
	}
	// node_type отделяет листья-чанки RAPTOR от узлов-сводок в одной коллекции; у старых точек
	// поля нет, и фильтр по нему их не находит.
	if err := q.ensurePayloadIndex(ctx, "node_type", keywordPayload); err != nil {
		return err
	}
	// order_by в Qdrant требует range-индекса, чтобы листать чанки в порядке документа.
	return q.ensurePayloadIndex(ctx, "chunk_index", "integer")
}

func (q *Qdrant) ensurePayloadIndex(ctx context.Context, field, schema string) error {
	body := map[string]any{"field_name": field, "field_schema": schema}
	err := q.do(ctx, http.MethodPut, "/collections/"+q.collection+"/index?wait=true", body, nil)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

func (q *Qdrant) Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	body := map[string]any{"points": points}
	return q.do(ctx, http.MethodPut, "/collections/"+q.collection+"/points?wait=true", body, nil)
}

type SearchFilter struct {
	Eq     map[string]string
	AnyOf  map[string][]string
	NoneOf map[string][]string
	// Хотя бы одно из условий EitherOf должно совпасть: Eq и AnyOf соединяются через И, а «открыто
	// всем ИЛИ принадлежит читателю» иначе не выразить.
	EitherOf map[string][]string
}

func (f SearchFilter) qdrant() map[string]any {
	must := make([]map[string]any, 0, len(f.Eq)+len(f.AnyOf))
	for k, v := range f.Eq {
		must = append(must, map[string]any{qKey: k, qMatch: map[string]any{qValue: v}})
	}
	must = append(must, anyConditions(f.AnyOf)...)
	filter := map[string]any{qMust: must}
	if should := anyConditions(f.EitherOf); len(should) > 0 {
		filter["should"] = should
		filter["min_should"] = map[string]any{"conditions": should, "min_count": 1}
	}
	if mustNot := anyConditions(f.NoneOf); len(mustNot) > 0 {
		filter["must_not"] = mustNot
	}
	return filter
}

func anyConditions(fields map[string][]string) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for k, vals := range fields {
		if len(vals) > 0 {
			out = append(out, map[string]any{qKey: k, qMatch: map[string]any{qAny: vals}})
		}
	}
	return out
}

func (f SearchFilter) empty() bool {
	return len(f.Eq) == 0 && len(f.AnyOf) == 0 && len(f.NoneOf) == 0 && len(f.EitherOf) == 0
}

func (q *Qdrant) Search(ctx context.Context, vector []float32, limit int, eq map[string]string) ([]SearchHit, error) {
	return q.SearchFiltered(ctx, vector, limit, SearchFilter{Eq: eq})
}

func (q *Qdrant) SearchFiltered(ctx context.Context, vector []float32, limit int, f SearchFilter) ([]SearchHit, error) {
	body := map[string]any{"vector": vector, "limit": limit, qWithPayload: true}
	if !f.empty() {
		body["filter"] = f.qdrant()
	}
	var resp struct {
		Result []SearchHit `json:"result"`
	}
	if err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/search", body, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

type ScrollPoint struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}

const scrollPageSize = 256

// scrollCursor: упорядоченный режим идёт по включительному start_from, неупорядоченный — по next_page_offset.
type scrollCursor struct {
	orderBy   string
	startFrom any
	offset    any
}

// В упорядоченном продолжении просим на одну точку больше: start_from включителен, граничная точка
// вернётся повторно и вызывающий её отрежет.
func (c *scrollCursor) body(must []map[string]any, want int) (map[string]any, int) {
	requested := want
	body := map[string]any{
		"filter":     map[string]any{qMust: must},
		qWithPayload: true,
	}
	switch {
	case c.orderBy != "" && c.startFrom != nil:
		requested = want + 1
		body["order_by"] = map[string]any{qKey: c.orderBy, qDirection: qAsc, "start_from": c.startFrom}
	case c.orderBy != "":
		body["order_by"] = map[string]any{qKey: c.orderBy, qDirection: qAsc}
	case c.offset != nil:
		body["offset"] = c.offset
	}
	body["limit"] = requested
	return body, requested
}

func (c *scrollCursor) advance(collected []ScrollPoint, nextPageOffset any) bool {
	if c.orderBy == "" {
		c.offset = nextPageOffset
		return nextPageOffset != nil
	}
	if len(collected) == 0 {
		return false
	}
	v, ok := collected[len(collected)-1].Payload[c.orderBy]
	c.startFrom = v
	return ok
}

func (q *Qdrant) Scroll(ctx context.Context, eq map[string]string, orderBy string, limit int) ([]ScrollPoint, error) {
	must := make([]map[string]any, 0, len(eq))
	for k, v := range eq {
		must = append(must, map[string]any{qKey: k, qMatch: map[string]any{qValue: v}})
	}

	out := make([]ScrollPoint, 0, min(limit, scrollPageSize))
	cur := scrollCursor{orderBy: orderBy, startFrom: nil, offset: nil}
	for len(out) < limit {
		body, requested := cur.body(must, min(scrollPageSize, limit-len(out)))
		var resp struct {
			Result struct {
				Points         []ScrollPoint `json:"points"`
				NextPageOffset any           `json:"next_page_offset"`
			} `json:"result"`
		}
		if err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/scroll", body, &resp); err != nil {
			return nil, err
		}
		got := len(resp.Result.Points)
		pts := resp.Result.Points
		if orderBy != "" && cur.startFrom != nil && got > 0 {
			pts = pts[1:] // граничный дубликат включительного start_from
		}
		out = append(out, pts...)
		if got < requested || !cur.advance(out, resp.Result.NextPageOffset) {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (q *Qdrant) Ping(ctx context.Context) error {
	return q.do(ctx, http.MethodGet, "/readyz", nil, nil)
}

func (q *Qdrant) do(ctx context.Context, method, path string, in, out any) error {
	var reader io.Reader
	if in != nil {
		raw, err := jsonx.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.base+path, reader)
	if err != nil {
		return fmt.Errorf("qdrant new request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := q.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &responseError{
			status: resp.StatusCode,
			message: fmt.Sprintf(
				"qdrant %s %s: status %d: %s",
				method,
				path,
				resp.StatusCode,
				string(
					raw,
				),
			),
		}
	}
	if out != nil {
		if derr := jsonx.NewDecoder(resp.Body).Decode(out); derr != nil {
			return fmt.Errorf("decode qdrant response: %w", derr)
		}
	}
	return nil
}

func (q *Qdrant) ensureCollectionSchema(ctx context.Context, dim int) error {
	var current collectionSchema
	err := q.do(ctx, http.MethodGet, "/collections/"+q.collection, nil, &current)
	if err == nil {
		return current.validate(dim)
	}
	failure, ok := errors.AsType[*responseError](err)
	if !ok || failure.status != http.StatusNotFound {
		return err
	}
	body := map[string]any{"vectors": map[string]any{"size": dim, "distance": cosineDistance}}
	err = q.do(ctx, http.MethodPut, "/collections/"+q.collection, body, nil)
	if err == nil {
		return nil
	}
	failure, ok = errors.AsType[*responseError](err)
	if !ok || (failure.status != http.StatusConflict && failure.status != http.StatusBadRequest) {
		return err
	}
	if err = q.do(ctx, http.MethodGet, "/collections/"+q.collection, nil, &current); err != nil {
		return err
	}
	return current.validate(dim)
}
