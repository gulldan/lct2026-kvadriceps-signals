package searchstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/example/llm-service/internal/platform/searchstore"
)

func TestIndexUnavailableDoesNotTriggerCreation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Error("unavailable index caused mutation")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if err := searchstore.NewOpenSearch(srv.URL, "test").EnsureIndex(t.Context()); err == nil {
		t.Fatal("unavailable index accepted")
	}
}

func TestOpenSearchSchemaLive(t *testing.T) {
	base := os.Getenv("OPENSEARCH_TEST_URL")
	if base == "" {
		t.Skip("OPENSEARCH_TEST_URL is required")
	}
	for _, bad := range []bool{false, true} {
		name := "qa-schema-" + uuid.NewV4().String()
		t.Cleanup(func() {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, base+"/"+name, nil)
			response, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = response.Body.Close()
			}
		})
		if bad {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, base+"/"+name,
				strings.NewReader(`{"mappings":{"properties":{"text":{"type":"keyword"}}}}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode >= 300 {
				t.Fatalf("fixture creation: %d", response.StatusCode)
			}
		}
		index := searchstore.NewOpenSearch(base, name)
		err := index.EnsureIndex(t.Context())
		if bad {
			if err == nil {
				t.Fatal("live incompatible mapping accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = index.EnsureIndex(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
