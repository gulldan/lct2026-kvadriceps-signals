package main

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestInvestorContextKeepsUnknownDateOutOfTrendCounts(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
			return
		}
		fmt.Fprint(w, webArticle("Hydrogen storage and industrial heat are among the technologies this fund describes as "+
			"research opportunities. This page states an investment thesis without announcing a transaction."))
	}))
	t.Cleanup(server.Close)
	source := webSource{Name: "Investor", URL: server.URL + "/thesis", Kind: kindURL, Genre: genreInvestmentThesis}
	s := webPanelService(t, server, []webSource{source})
	data := &snapshot{Source: kindWeb, SourceSet: "investor-test", From: "2026-10-01", Through: "2026-10-18",
		ObservedAt: "2026-10-18T01:00:00Z"}
	s.sourceHistory(t.Context(), source, data, nil)
	if len(data.Records) != 1 {
		t.Fatalf("missing thesis: %+v", data.Coverage)
	}
	row := data.Records[0]
	if row.Genre != genreInvestmentThesis || row.Date != "" || row.PublishedAt != "" || row.ObservedAt != data.ObservedAt ||
		row.TextStatus != articleText || !inSnapshotWindow(row, data, time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("thesis date or genre changed: %+v", row)
	}
	// Повторно используемая страница обязана получить текущий жанр конфигурации.
	row.Genre = ""
	s.sourceHistory(t.Context(), source, data, map[string]record{row.ID: row})
	if data.Records[1].Genre != genreInvestmentThesis {
		t.Fatal("cached page lost source genre")
	}
	data.Records = []record{data.Records[0], {ID: "dated", Title: "Hydrogen analysis", Date: "2026-10-10"},
		{ID: "old", Title: "Hydrogen context", ObservedAt: "2026-09-30T01:00:00Z"}}
	summarizeCoverage(data)
	if data.Coverage[0].Undated != 1 || data.Coverage[0].Received != 0 {
		t.Fatal("undated context was counted as dated coverage")
	}
	s.data = data
	for _, tc := range []struct {
		flag  string
		total int
	}{
		{"", 1}, {"&include_undated=0", 1}, {"&include_undated=1", 2},
		{"&genre=investment_thesis", 0}, {"&genre=investment_thesis&include_undated=1", 1},
		{"&genre=investor_commentary&include_undated=1", 0},
	} {
		w := httptest.NewRecorder()
		s.exportRecords(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/v1/records?limit=100&from=2026-10-01&through=2026-10-18&query=*"+tc.flag, nil))
		var result struct {
			Total   int      `json:"total"`
			Records []record `json:"records"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result.Total != tc.total {
			t.Fatalf("export %q: %s (%v)", tc.flag, w.Body.String(), err)
		}
		for _, exported := range result.Records {
			if exported.Genre == genreInvestmentThesis && (exported.Date != "" || exported.PublishedAt != "") {
				t.Fatal("export invented a publication date")
			}
		}
	}
	for _, flag := range []string{"&genre=funding", "&include_undated=true"} {
		w := httptest.NewRecorder()
		s.exportRecords(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/v1/records?limit=100&from=2026-10-01&through=2026-10-18&query=*"+flag, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid export option accepted: %s", flag)
		}
	}
	for _, flag := range []string{"&genre=investment_thesis", "&include_undated=1"} {
		w := httptest.NewRecorder()
		patents := &service{kind: kindPatents, data: data}
		patents.exportRecords(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/v1/records?limit=100&from=2026-10-01&through=2026-10-18&query=*"+flag, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("web export option accepted by patent service: %s", flag)
		}
	}
	w := httptest.NewRecorder()
	s.query(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/query", strings.NewReader(
		`{"terms":["hydrogen"],"periods":[{"start":"2026-10-01","end_exclusive":"2026-11-01"}]}`)))
	var result struct {
		Total  int   `json:"total"`
		Counts []int `json:"counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Total != 1 || !slices.Equal(result.Counts, []int{1}) {
		t.Fatalf("undated context changed trends: %s (%v)", w.Body.String(), err)
	}
}

func TestInvestorPanelConfiguration(t *testing.T) {
	for _, path := range []string{"../../infra/trend-sources.json", "source-config.example.json"} {
		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatal(path, err)
		}
		for _, expected := range append(investorSources(), ventureResearchSources()...) {
			at := slices.IndexFunc(cfg.Web, func(source webSource) bool { return source.Name == expected.Name })
			if at < 0 || cfg.Web[at].URL != expected.URL || cfg.Web[at].Genre != expected.Genre ||
				cfg.Web[at].Industry != expected.Industry || !slices.Equal(cfg.Web[at].URLs, expected.URLs) {
				t.Fatalf("%s differs from built-in panel: %s", path, expected.Name)
			}
		}
	}
	for _, source := range []webSource{
		{Kind: kindCrossref, Genre: genreInvestmentThesis}, {Kind: kindURL, Genre: "funding"},
		{Kind: kindURL, Genre: "scientific_paper"},
	} {
		if validInvestorGenre(source) {
			t.Fatalf("misleading genre accepted: %+v", source)
		}
	}
}

func TestLiveInvestorSources(t *testing.T) {
	if os.Getenv("TR_LIVE_INVESTORS") != "1" {
		t.Skip("set TR_LIVE_INVESTORS=1 and TR_LIVE_SOURCES=1 for the public investor panel")
	}
	s, data, ctx := liveSourceService(t, kindWeb)
	s.config.RequestBudget = 140
	data.From = time.Now().UTC().AddDate(0, -13, 0).Format(time.DateOnly)
	proof := []map[string]any{}
	for _, source := range append(investorSources(), ventureResearchSources()...) {
		t.Run(source.Name, func(t *testing.T) {
			start := len(data.Records)
			s.sourceHistory(ctx, source, data, nil)
			rows := data.Records[start:]
			withText, undated := 0, 0
			for _, row := range rows {
				if row.Text != "" {
					withText++
				}
				if row.Date == "" {
					undated++
				}
				if source.Genre != "" && row.Genre != source.Genre {
					t.Fatalf("genre lost for %s", row.URL)
				}
			}
			c := data.Coverage[len(data.Coverage)-1]
			item := map[string]any{"source": source.Name, "url": source.URL, "kind": source.Kind,
				"genre": source.Genre, "records": len(rows), "with_text": withText, "undated": undated,
				"coverage": c}
			if len(rows) > 0 {
				item["sample"] = queryPreview(rows[0], request{})
			}
			proof = append(proof, item)
			if len(rows) == 0 || source.Genre != "" && withText == 0 {
				t.Fatalf("no usable records: %+v", c)
			}
			t.Logf("records=%d text=%d undated=%d", len(rows), withText, undated)
		})
	}
	if err := atomicSourceJSON(filepath.Join(s.dir, "investor-proof.json"), proof); err != nil {
		t.Fatal(err)
	}
}
