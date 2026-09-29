package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCNBadCachedDocumentDoesNotStarveLaterPublication(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile("testdata/cn-122065940-a.html")
	if err != nil {
		t.Fatal(err)
	}
	s := &service{kind: kindPatents, dir: t.TempDir(), config: defaultConfig()}
	s.config.CNDocuments = 1
	s.http = &http.Client{Transport: ledgerTransport(func(req *http.Request) (*http.Response, error) {
		body := "malformed publication"
		if req.URL.Path == "/robots.txt" {
			body = "User-agent: *\nAllow: /patent/\n"
		}
		if strings.Contains(req.URL.Path, "CN122065940A") {
			body = string(fixture)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	known := map[string]record{}
	for pass := range 2 {
		data := &snapshot{From: "2025-08-01", Through: "2026-09-13", ObservedAt: "2026-09-13T00:00:00Z"}
		c, read := &coverage{}, 0
		if s.cnDocument(t.Context(), cnCandidate{ID: "CN122065939A"}, data, c, known, &read) {
			t.Fatal("malformed publication stopped the pass")
		}
		stopped := s.cnDocument(t.Context(), cnCandidate{ID: "CN122065940A", Title: "Battery thermal runaway sensor"},
			data, c, known, &read)
		if stopped || len(data.Records) != pass {
			t.Fatalf("cached refusal consumed next budget: pass=%d stopped=%v rows=%d", pass, stopped, len(data.Records))
		}
		if pass == 1 && (data.Records[0].Title != "Battery thermal runaway sensor" || data.Records[0].OriginalTitle == "") {
			t.Fatalf("английский заголовок не применён: %+v", data.Records[0])
		}
	}
}

func TestCNLiveCollection(t *testing.T) {
	dir := os.Getenv("CN_LIVE_DIR")
	if dir == "" {
		t.Skip("set CN_LIVE_DIR to an isolated output directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.CNDocuments, cfg.RequestBudget = 3, 30
	cfg.PatentTerms = []string{"large language model"}
	s := &service{kind: kindPatents, dir: dir, config: cfg, http: newWebClient(35*time.Second, collectorDomains(cfg, kindPatents))}
	data := &snapshot{From: "2025-08-01", Through: time.Now().UTC().Format(time.DateOnly),
		ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	s.cnHistory(ctx, data)
	if len(data.Records) == 0 {
		t.Fatalf("CN discovery returned no documents: %+v", data.Coverage)
	}
	if err := atomicSourceJSON(filepath.Join(dir, "cn-proof.json"), data); err != nil {
		t.Fatal(err)
	}
	for _, row := range data.Records {
		t.Logf("CN publication=%s date=%s language=%s bytes=%d sections=%d claims=%d hash=%s",
			row.ID, row.Date, row.Language, len(row.Text), len(row.FullText.Sections), len(row.FullText.Claims), row.ContentHash)
	}
}
