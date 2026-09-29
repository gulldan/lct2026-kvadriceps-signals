package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// Повторный заход не должен везти тело заново: список тем меняется раз в сутки,
// а весит больше мегабайта.
func TestClustersRevalidateWithoutBody(t *testing.T) {
	h := newHarness(t)
	h.cat.clusters["theme"] = &commonv1.Cluster{
		Id: "theme", OwnerId: "alice", Label: "Materials",
		Params: `{"fingerprint":"stable","itc":{"techscore":4,"axes":{"novelty":0.8,"impact":0.3}}}`,
	}
	token := h.token(t, "alice")

	first := h.do(t, http.MethodGet, "/clusters", token, "")
	etag := first.header.Get("ETag")
	if first.code != http.StatusOK || etag == "" {
		t.Fatalf("list = %d, etag = %q", first.code, etag)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, h.server.URL+"/clusters", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-None-Match", etag)
	if second := h.run(t, req); second.code != http.StatusNotModified || second.body != "" {
		t.Fatalf("revalidation = %d body %q, want 304 without body", second.code, second.body)
	}
}
