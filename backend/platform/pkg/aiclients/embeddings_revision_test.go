package aiclients

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPinnedEmbeddingRejectsChangedAndUnversionedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, model, revision, expected string
		fail                            bool
	}{
		{"matching", "model", "weights-v1", "weights-v1", false},
		{"wrong weights", "model", "weights-v2", "weights-v1", true},
		{"wrong model", "another", "weights-v1", "weights-v1", true},
		{"old server", "model", "", "weights-v1", true},
		{"empty pin", "model", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"model":%q,"revision":%q,"data":[{"embedding":[0.1,0.2]}]}`, tc.model, tc.revision)
			}))
			defer server.Close()
			embedder := NewVersionedEmbedder(server.URL, "model", tc.expected, 2, nil)
			vectors, err := embedder.Embed(t.Context(), []string{"evidence"})
			if (err != nil) != tc.fail || (!tc.fail && len(vectors) != 1) {
				t.Fatalf("vectors=%v err=%v", vectors, err)
			}
		})
	}
}
