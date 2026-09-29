package main

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCNCollectorPublishesWithoutUSOrEP(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile("testdata/cn-122065940-a.html")
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.CNDocuments, cfg.RequestBudget = 1, 10
	cfg.PatentTerms = []string{"large language model"}
	calls := map[string]int{}
	s := &service{kind: kindCNPatents, dir: t.TempDir(), config: cfg}
	s.http = &http.Client{Transport: ledgerTransport(func(req *http.Request) (*http.Response, error) {
		calls[req.URL.Host+req.URL.Path]++
		if req.URL.Host != "patents.google.com" {
			t.Errorf("isolated CN collection called another provider: %s", req.URL)
			return nil, fmt.Errorf("unexpected provider: %s", req.URL.Host)
		}
		var body string
		switch req.URL.Path {
		case "/robots.txt":
			body = "User-agent: *\nAllow: /sitemap/\nAllow: /patent/\n"
		case "/sitemap/":
			body = `<a href="2026-W21.html">week</a>`
		case "/sitemap/2026-W21.html":
			body = `<li>CN122065940A - Large language model knowledge extraction : <a href="ignored">zh</a></li>`
		case "/patent/CN122065940A/zh":
			body = string(fixture)
		default:
			t.Errorf("unexpected CN request: %s", req.URL)
			return nil, fmt.Errorf("unexpected CN path: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	for range 2 {
		if err := s.collect(t.Context(), time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, "snapshot.json"))
		if err != nil {
			t.Fatal(err)
		}
		var saved snapshot
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Source != cnSource || saved.PatentMetadata != nil || len(saved.Coverage) != 1 ||
			saved.Coverage[0].Source != cnSource || len(saved.Records) != 1 {
			t.Fatalf("CN snapshot waited for or included another jurisdiction: %+v", saved)
		}
		if row := saved.Records[0]; row.ID != "CN122065940A" || row.Language != "zh" || row.Text == "" {
			t.Fatalf("CN original was not published intact: %s, %s", row.ID, row.Language)
		}
	}
	if calls["patents.google.com/patent/CN122065940A/zh"] != 1 {
		t.Fatalf("repeat collection fetched an unchanged CN publication again: %v", calls)
	}
}
