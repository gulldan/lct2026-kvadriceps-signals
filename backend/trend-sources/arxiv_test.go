package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestArxivCooldownSurvivesRestartAndDoesNotContactUpstream(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "180")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	g := newArxivProxy(dir)
	g.upstream = server.URL
	g.now = func() time.Time { return now }
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/query?search_query=hydrogen", nil)
	w := httptest.NewRecorder()
	g.query(w, request)
	if w.Code != 429 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	restarted := newArxivProxy(dir)
	restarted.upstream = server.URL
	restarted.now = g.now
	w = httptest.NewRecorder()
	restarted.query(w, request)
	if w.Code != 429 || calls != 1 || w.Header().Get("Retry-After") == "" {
		t.Fatal("ignored shared cooldown", w.Code, calls)
	}
}
func TestArxivClosedWindowExactCacheAndExpiry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:o="http://a9.com/-/spec/opensearch/1.1/"><`+
			`o:totalResults>1</o:totalResults><o:startIndex>0</o:startIndex><entry><id>http://arxiv.org/abs/2508.00001v1</id><`+
			`/entry></feed>`)
	}))
	defer server.Close()
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	g := newArxivProxy(t.TempDir())
	g.upstream = server.URL
	g.now = func() time.Time { return now }
	q := url.Values{"search_query": {"hydrogen AND submittedDate:[202508010000 TO 202508312359]"}, "start": {"0"},
		"max_results": {"10"}}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/query?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	g.query(w, request)
	if w.Code != 200 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	w = httptest.NewRecorder()
	g.query(w, request)
	if w.Header().Get("X-Radar-Cache") != "fresh" || calls != 1 {
		t.Fatal("fresh exact cache missed", calls)
	}
	q.Set("search_query", "another AND submittedDate:[202508010000 TO 202508312359]")
	if _, _, ok := g.readCache(hashBytes([]byte(q.Encode()))); ok {
		t.Fatal("invented missing query cache")
	}
	now = now.Add(25 * time.Hour)
	if _, _, ok := g.readCache(hashBytes([]byte(request.URL.Query().Encode()))); ok {
		t.Fatal("stale page used as fresh")
	}
	q.Set("search_query", "submittedDate:[202609010000 TO 202609112359]")
	if closedArxivWindow(q, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("partial current month frozen")
	}
}
