package vectorstore_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"uuid"

	"github.com/example/chunk-splitter/internal/platform/vectorstore"
)

func TestCollectionRejectsDriftAndAvailabilityErrorsBeforeMutation(t *testing.T) {
	for _, scenario := range []struct {
		status int
		body   string
	}{
		{http.StatusServiceUnavailable, `{"error":"maintenance"}`},
		{http.StatusUnauthorized, `{}`},
		{http.StatusOK, `{"result":{"config":{"params":{"vectors":{"size":3,"distance":"Cosine"}}}}}`},
		{http.StatusOK, `{"result":{"config":{"params":{"vectors":{"size":2,"distance":"Dot"}}}}}`},
		{
			http.StatusOK,
			`{"result":{"config":{"params":{"vectors":{"size":2,"distance":"Cosine"}}},
 "payload_schema":{"owner_id":{"data_type":"text"}}}}`,
		},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("schema failure caused mutation")
			}
			w.WriteHeader(scenario.status)
			_, _ = fmt.Fprint(w, scenario.body)
		}))
		if err := vectorstore.NewQdrant(srv.URL, "test").EnsureCollection(t.Context(), 2); err == nil {
			t.Fatal("incompatible collection accepted")
		}
		srv.Close()
	}
}

func TestQdrantSchemaLive(t *testing.T) {
	base := os.Getenv("QDRANT_TEST_URL")
	if base == "" {
		t.Skip("QDRANT_TEST_URL is required")
	}
	name := "qa-schema-" + uuid.NewV4().String()
	q := vectorstore.NewQdrant(base, name)
	t.Cleanup(func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, base+"/collections/"+name, nil)
		response, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = response.Body.Close()
		}
	})
	for range 2 {
		if err := q.EnsureCollection(t.Context(), 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.EnsureCollection(t.Context(), 3); err == nil {
		t.Fatal("live dimension mismatch accepted")
	}
}
