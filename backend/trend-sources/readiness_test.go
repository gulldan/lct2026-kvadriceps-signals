package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArxivGatewayReadyAfterCacheInitialization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "arxiv")
	handler, err := newArxivGatewayHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("new gateway cache must exist without a collector snapshot: entries=%v err=%v", entries, err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
		if response.Code != http.StatusOK {
			t.Fatalf("initialized arxiv gateway %s=%d, want 200", path, response.Code)
		}
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err = os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if handler, err = newArxivGatewayHandler(blocked); err == nil || handler != nil {
		t.Fatal("failed cache initialization must not publish a ready handler")
	}
}

func TestReadinessKeepsUsableSnapshotDuringRetry(t *testing.T) {
	s := &service{}
	for _, tc := range []struct {
		snapshot *snapshot
		want     int
	}{
		{nil, http.StatusServiceUnavailable},
		{&snapshot{ObservedAt: "invalid"}, http.StatusServiceUnavailable},
		{&snapshot{ObservedAt: time.Now().Add(-73 * time.Hour).Format(time.RFC3339)}, http.StatusServiceUnavailable},
		{&snapshot{ObservedAt: time.Now().Format(time.RFC3339)}, http.StatusOK},
	} {
		s.data, s.lastError = tc.snapshot, "provider unavailable"
		w := httptest.NewRecorder()
		s.ready(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))
		if w.Code != tc.want {
			t.Fatalf("readiness=%d want %d", w.Code, tc.want)
		}
	}
	if collectionDelay(6*time.Hour, 0) != 6*time.Hour || collectionDelay(6*time.Hour, 1) != time.Minute ||
		collectionDelay(6*time.Hour, 100) != 16*time.Minute {
		t.Fatal("collection retry is not bounded or does not reset")
	}
}
