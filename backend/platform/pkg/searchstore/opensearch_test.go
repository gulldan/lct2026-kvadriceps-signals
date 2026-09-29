package searchstore_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/rag-mvp/pkg/jsonx"
	"github.com/example/rag-mvp/pkg/searchstore"
)

const testIndex = "chunks"

type recorder struct {
	method      string
	path        string
	contentType string
	body        []byte
}

func newServer(t *testing.T, rec *recorder, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.contentType = r.Header.Get("Content-Type")
		rec.body, _ = io.ReadAll(r.Body)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnsureIndexCreates(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	if err := c.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if rec.method != http.MethodPut {
		t.Errorf("create method = %q, want PUT", rec.method)
	}
	if rec.path != "/"+testIndex {
		t.Errorf("create path = %q, want /%s", rec.path, testIndex)
	}
	var mapping map[string]any
	if err := jsonx.Unmarshal(rec.body, &mapping); err != nil {
		t.Fatalf("decode mapping body: %v", err)
	}
	if _, ok := mapping["mappings"]; !ok {
		t.Errorf("create body missing mappings: %s", rec.body)
	}
	if _, ok := mapping["settings"]; !ok {
		t.Errorf("create body missing settings: %s", rec.body)
	}
}

func TestEnsureIndexAlreadyExists(t *testing.T) {
	var rec recorder
	var definition map[string]any
	exists := false
	srv := newServer(t, &rec, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead:
			if !exists {
				w.WriteHeader(http.StatusNotFound)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/"+testIndex:
			if err := jsonx.Unmarshal(rec.body, &definition); err != nil {
				t.Error(err)
			}
			exists = true
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			_ = jsonx.NewEncoder(w).Encode(map[string]any{testIndex: map[string]any{"mappings": definition["mappings"]}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/_settings"):
			settings, ok := definition["settings"].(map[string]any)
			if !ok {
				t.Fatal("settings missing")
			}
			analysis := settings["analysis"]
			_ = jsonx.NewEncoder(
				w,
			).Encode(
				map[string]any{
					testIndex: map[string]any{
						"settings": map[string]any{
							"index": map[string]any{
								"analysis": analysis,
							},
						},
					},
				},
			)
		}
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	for range 2 {
		if err := c.EnsureIndex(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if rec.method != http.MethodPut || rec.path != "/"+testIndex+"/_settings" {
		t.Fatalf("settings update: %+v", rec)
	}
	mappings, ok := definition["mappings"].(map[string]any)
	if !ok {
		t.Fatal("mappings missing")
	}
	properties, ok := mappings["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing")
	}
	text, ok := properties["text"].(map[string]any)
	if !ok {
		t.Fatal("text mapping missing")
	}
	text["analyzer"] = "standard"
	if err := c.EnsureIndex(t.Context()); err == nil {
		t.Fatal("incompatible analyzer accepted")
	}
	if rec.method != http.MethodGet {
		t.Fatal("incompatible schema mutated")
	}
}

func TestEnsureIndexHeadError(t *testing.T) {
	c := searchstore.NewOpenSearch("http://127.0.0.1:0", testIndex)
	if err := c.EnsureIndex(context.Background()); err == nil {
		t.Fatal("EnsureIndex: want transport error, got nil")
	}
}

func TestEnsureIndexCreateNon2xx(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	err := c.EnsureIndex(context.Background())
	if err == nil {
		t.Fatal("EnsureIndex: want error, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %v, want status 500", err)
	}
}

func TestIndex(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	doc := searchstore.Doc{
		ID:         "abc",
		Text:       "lithium iron phosphate",
		DocumentID: "doc-1",
		OwnerID:    "owner-1",
		Filename:   "paper.pdf",
		Metadata:   map[string]string{"lang": "en"},
	}
	if err := c.Index(context.Background(), doc); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", rec.method)
	}
	if rec.path != "/"+testIndex+"/_doc/abc" {
		t.Errorf("path = %q, want /%s/_doc/abc", rec.path, testIndex)
	}
	if rec.contentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", rec.contentType)
	}
	var got searchstore.Doc
	if err := jsonx.Unmarshal(rec.body, &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Text != doc.Text || got.DocumentID != doc.DocumentID || got.Filename != doc.Filename {
		t.Errorf("body = %+v, want %+v", got, doc)
	}
}

func TestIndexNon2xx(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad mapping"))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	err := c.Index(context.Background(), searchstore.Doc{ID: "x"})
	if err == nil {
		t.Fatal("Index: want error, got nil")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %v, want status 400", err)
	}
}

func TestBulkIndex(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	docs := []searchstore.Doc{
		{ID: "a", Text: "first", DocumentID: "d1"},
		{ID: "b", Text: "second", DocumentID: "d2"},
	}
	if err := c.BulkIndex(context.Background(), docs); err != nil {
		t.Fatalf("BulkIndex: %v", err)
	}
	if rec.method != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.method)
	}
	if rec.path != "/_bulk" {
		t.Errorf("path = %q, want /_bulk", rec.path)
	}
	if rec.contentType != "application/x-ndjson" {
		t.Errorf("content-type = %q, want application/x-ndjson", rec.contentType)
	}
	lines := splitNDJSON(t, rec.body)
	if len(lines) != 4 {
		t.Fatalf("ndjson lines = %d, want 4 (meta+doc per document)", len(lines))
	}
	var meta struct {
		Index struct {
			Index string `json:"_index"`
			ID    string `json:"_id"`
		} `json:"index"`
	}
	if err := jsonx.Unmarshal(lines[0], &meta); err != nil {
		t.Fatalf("decode meta line: %v", err)
	}
	if meta.Index.Index != testIndex || meta.Index.ID != "a" {
		t.Errorf("meta = %+v, want index=%s id=a", meta.Index, testIndex)
	}
	var doc searchstore.Doc
	if err := jsonx.Unmarshal(lines[1], &doc); err != nil {
		t.Fatalf("decode doc line: %v", err)
	}
	if doc.Text != "first" {
		t.Errorf("doc line text = %q, want first", doc.Text)
	}
}

func TestBulkIndexEmpty(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	if err := c.BulkIndex(context.Background(), nil); err != nil {
		t.Fatalf("BulkIndex(nil): %v", err)
	}
	if called {
		t.Error("BulkIndex(nil) issued a request, want none")
	}
}

func TestBulkIndexItemError(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":true,"items":[{"index":{"status":400,` +
			`"error":{"type":"mapper_parsing_exception","reason":"bad field"}}}]}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	err := c.BulkIndex(context.Background(), []searchstore.Doc{{ID: "a", Text: "x"}})
	if err == nil {
		t.Fatal("BulkIndex: want item error, got nil")
	}
	if !strings.Contains(err.Error(), "mapper_parsing_exception") || !strings.Contains(err.Error(), "bad field") {
		t.Errorf("error = %v, want mapper_parsing_exception/bad field", err)
	}
}

func TestBulkIndexErrorsWithoutItem(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":true,"items":[{"index":{"status":200}}]}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	err := c.BulkIndex(context.Background(), []searchstore.Doc{{ID: "a"}})
	if err == nil {
		t.Fatal("BulkIndex: want error, got nil")
	}
	if !strings.Contains(err.Error(), "reported errors") {
		t.Errorf("error = %v, want reported errors", err)
	}
}

func TestBulkIndexNon2xx(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("overloaded"))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	err := c.BulkIndex(context.Background(), []searchstore.Doc{{ID: "a"}})
	if err == nil {
		t.Fatal("BulkIndex: want error, got nil")
	}
	if !strings.Contains(err.Error(), "status 503") {
		t.Errorf("error = %v, want status 503", err)
	}
}

func TestBulkIndexMalformedJSON(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not json"))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	err := c.BulkIndex(context.Background(), []searchstore.Doc{{ID: "a"}})
	if err == nil {
		t.Fatal("BulkIndex: want decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %v, want decode error", err)
	}
}

func TestBulkIndexContextCancelled(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":false}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.BulkIndex(ctx, []searchstore.Doc{{ID: "a"}}); err == nil {
		t.Fatal("BulkIndex: want context error, got nil")
	}
}

func TestSearchParsesHits(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hits":{"hits":[
			{"_id":"a","_score":2.5,"_source":{"text":"alpha","document_id":"d1","filename":"a.pdf"}},
			{"_id":"b","_score":1.25,"_source":{"text":"beta","document_id":"d2","filename":"b.pdf"}}
		]}}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	hits, err := c.Search(context.Background(), "energy", "owner-1", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/"+testIndex+"/_search" {
		t.Errorf("request = %s %s, want POST /%s/_search", rec.method, rec.path, testIndex)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].ID != "a" || hits[0].Score != 2.5 || hits[0].Text != "alpha" ||
		hits[0].DocumentID != "d1" || hits[0].Filename != "a.pdf" {
		t.Errorf("hit[0] = %+v, want id=a score=2.5 text=alpha", hits[0])
	}
	if hits[1].ID != "b" || hits[1].Score != 1.25 {
		t.Errorf("hit[1] = %+v, want id=b score=1.25", hits[1])
	}
	if !bytes.Contains(rec.body, []byte("owner_id")) {
		t.Errorf("query body missing owner_id filter: %s", rec.body)
	}
	if !bytes.Contains(rec.body, []byte("multi_match")) {
		t.Errorf("query body missing multi_match: %s", rec.body)
	}
}

func TestSearchNoOwnerFilter(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hits":{"hits":[]}}`))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	hits, err := c.Search(context.Background(), "energy", "", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %d, want 0", len(hits))
	}
	if bytes.Contains(rec.body, []byte("owner_id")) {
		t.Errorf("shared-corpus query must not filter on owner_id: %s", rec.body)
	}
	if bytes.Contains(rec.body, []byte(`"filter"`)) {
		t.Errorf("shared-corpus query must not carry a filter clause: %s", rec.body)
	}
}

func TestSearchMalformedJSON(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	_, err := c.Search(context.Background(), "q", "", 5)
	if err == nil {
		t.Fatal("Search: want decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %v, want decode error", err)
	}
}

func TestSearchNon2xx(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("search broke"))
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	_, err := c.Search(context.Background(), "q", "owner", 5)
	if err == nil {
		t.Fatal("Search: want error, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %v, want status 500", err)
	}
}

func TestPing(t *testing.T) {
	var rec recorder
	srv := newServer(t, &rec, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c := searchstore.NewOpenSearch(srv.URL, testIndex)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if rec.method != http.MethodGet || rec.path != "/" {
		t.Errorf("request = %s %s, want GET /", rec.method, rec.path)
	}
}

func TestPingUnreachable(t *testing.T) {
	c := searchstore.NewOpenSearch("http://127.0.0.1:0", testIndex)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping: want transport error, got nil")
	}
}

func splitNDJSON(t *testing.T, body []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan ndjson: %v", err)
	}
	return lines
}
