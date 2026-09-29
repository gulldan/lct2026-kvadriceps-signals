package retrieval_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/infrastructure/retrieval"
	"github.com/example/llm-service/internal/platform/searchstore"
	"github.com/example/llm-service/internal/platform/vectorstore"
)

func jsonStub(t *testing.T, body string, gotBody *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotBody != nil {
			buf, _ := io.ReadAll(r.Body)
			*gotBody = string(buf)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestQdrantRetrieve(t *testing.T) {
	const resp = `{"result":[{"id":"c1","score":0.87,"payload":{
		"text":"turbine blade","document_id":"d1","filename":"r.pdf",
		"chunk_index":3,"char_start":10,"char_end":20,
		"page_start":1,"page_end":2,"section_heading":"Intro"}}]}`
	var sent string
	srv := jsonStub(t, resp, &sent)
	r := retrieval.NewQdrant(vectorstore.NewQdrant(srv.URL, "documents"))

	hits, err := r.Retrieve(context.Background(), "", []float32{0.1, 0.2}, domain.RetrievalFilter{OwnerID: "owner-7"}, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	h := hits[0]
	if h.ID != "c1" || h.Text != "turbine blade" || h.DocumentID != "d1" || h.Filename != "r.pdf" {
		t.Fatalf("core fields wrong: %+v", h)
	}
	if h.ChunkIndex != 3 || h.CharStart != 10 || h.CharEnd != 20 || h.PageStart != 1 || h.PageEnd != 2 {
		t.Fatalf("int payload fields wrong: %+v", h)
	}
	// Балл Qdrant приходит как float32: сравниваем с тем же расширением, а не с литералом.
	if h.SectionHeading != "Intro" || h.Score != float64(float32(0.87)) {
		t.Fatalf("heading/score wrong: %+v", h)
	}
	if !strings.Contains(sent, "owner-7") {
		t.Fatalf("owner filter not sent in body: %s", sent)
	}
}

func TestQdrantRetrieveShared(t *testing.T) {
	var sent string
	srv := jsonStub(t, `{"result":[]}`, &sent)
	r := retrieval.NewQdrant(vectorstore.NewQdrant(srv.URL, "documents"))
	if _, err := r.Retrieve(context.Background(), "", []float32{0.1}, domain.RetrievalFilter{}, 5); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if strings.Contains(sent, "owner_id") {
		t.Fatalf("shared-corpus search must not filter on owner_id: %s", sent)
	}
}

func TestQdrantRetrieveSkips(t *testing.T) {
	r := retrieval.NewQdrant(vectorstore.NewQdrant("http://127.0.0.1:1", "documents"))
	ctx := context.Background()
	if hits, err := r.Retrieve(ctx, "", nil, domain.RetrievalFilter{OwnerID: "o"}, 5); err != nil || hits != nil {
		t.Fatalf("empty embedding: got (%v, %v), want (nil, nil)", hits, err)
	}
	if hits, err := r.Retrieve(ctx, "", []float32{0.1}, domain.RetrievalFilter{OwnerID: "o"}, 0); err != nil || hits != nil {
		t.Fatalf("topN=0: got (%v, %v), want (nil, nil)", hits, err)
	}
}

func TestQdrantRetrieveError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	r := retrieval.NewQdrant(vectorstore.NewQdrant(srv.URL, "documents"))
	if _, err := r.Retrieve(context.Background(), "", []float32{0.1}, domain.RetrievalFilter{OwnerID: "o"}, 5); err == nil {
		t.Fatal("expected an error on a 500 response")
	}
}

func TestOpenSearchRetrieve(t *testing.T) {
	const resp = `{"hits":{"hits":[
		{"_id":"c9","_score":4.5,"_source":{"text":"alloy grade","document_id":"d2","filename":"spec.pdf"}}]}}`
	var sent string
	srv := jsonStub(t, resp, &sent)
	r := retrieval.NewOpenSearch(searchstore.NewOpenSearch(srv.URL, "chunks"))

	hits, err := r.Retrieve(context.Background(), "alloy", nil, domain.RetrievalFilter{OwnerID: "owner-3"}, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	h := hits[0]
	if h.ID != "c9" || h.Text != "alloy grade" || h.DocumentID != "d2" || h.Filename != "spec.pdf" || h.Score != 4.5 {
		t.Fatalf("mapped hit wrong: %+v", h)
	}
	if !strings.Contains(sent, "owner-3") {
		t.Fatalf("owner filter not sent: %s", sent)
	}
}

func TestOpenSearchRetrieveSkips(t *testing.T) {
	r := retrieval.NewOpenSearch(searchstore.NewOpenSearch("http://127.0.0.1:1", "chunks"))
	ctx := context.Background()
	if hits, err := r.Retrieve(ctx, "", nil, domain.RetrievalFilter{OwnerID: "o"}, 5); err != nil || hits != nil {
		t.Fatalf("empty query: got (%v, %v), want (nil, nil)", hits, err)
	}
	if hits, err := r.Retrieve(ctx, "q", nil, domain.RetrievalFilter{OwnerID: "o"}, 0); err != nil || hits != nil {
		t.Fatalf("topN=0: got (%v, %v), want (nil, nil)", hits, err)
	}
}

func TestOpenSearchRetrieveError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	r := retrieval.NewOpenSearch(searchstore.NewOpenSearch(srv.URL, "chunks"))
	if _, err := r.Retrieve(context.Background(), "q", nil, domain.RetrievalFilter{OwnerID: "o"}, 5); err == nil {
		t.Fatal("expected an error on a 502 response")
	}
}

func TestChunkReaderDocumentChunks(t *testing.T) {
	// Порядок нарочно перемешан: читатель сортирует по chunk_index.
	const resp = `{"result":{"points":[
		{"id":"c2","payload":{"chunk_index":2,"text":"second"}},
		{"id":"c0","payload":{"chunk_index":0,"text":"first"}}],"next_page_offset":null}}`
	var sent string
	srv := jsonStub(t, resp, &sent)
	cr := retrieval.NewChunkReader(vectorstore.NewQdrant(srv.URL, "documents"))

	chunks, err := cr.DocumentChunks(context.Background(), "owner-1", "doc-1")
	if err != nil {
		t.Fatalf("DocumentChunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Index != 0 || chunks[0].Text != "first" || chunks[1].Index != 2 {
		t.Fatalf("chunks not sorted by index: %+v", chunks)
	}
	if !strings.Contains(sent, "doc-1") || !strings.Contains(sent, "owner-1") {
		t.Fatalf("document/owner filter not sent: %s", sent)
	}
}

func TestChunkReaderShared(t *testing.T) {
	var sent string
	srv := jsonStub(t, `{"result":{"points":[],"next_page_offset":null}}`, &sent)
	cr := retrieval.NewChunkReader(vectorstore.NewQdrant(srv.URL, "documents"))
	if _, err := cr.DocumentChunks(context.Background(), "", "doc-9"); err != nil {
		t.Fatalf("DocumentChunks: %v", err)
	}
	if strings.Contains(sent, "owner_id") {
		t.Fatalf("shared read must not filter on owner_id: %s", sent)
	}
}

func TestChunkReaderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	cr := retrieval.NewChunkReader(vectorstore.NewQdrant(srv.URL, "documents"))
	if _, err := cr.DocumentChunks(context.Background(), "o", "d"); err == nil {
		t.Fatal("expected an error on a 500 response")
	}
}
