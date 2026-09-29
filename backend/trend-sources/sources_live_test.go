package main

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func liveSourceService(t *testing.T, kind string) (*service, *snapshot, context.Context) {
	t.Helper()
	if os.Getenv("TR_LIVE_SOURCES") != "1" {
		t.Skip("set TR_LIVE_SOURCES=1 for public source verification")
	}
	dir := t.TempDir()
	if root := os.Getenv("TR_SOURCE_PROOF_DIR"); root != "" {
		if err := os.MkdirAll(root, 0750); err != nil {
			t.Fatal(err)
		}
		var err error
		dir, err = os.MkdirTemp(root, kind+"-")
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg := defaultConfig()
	cfg.RequestBudget = 35
	cfg.EPDocuments = 2
	s := &service{kind: kind, dir: dir, config: cfg, http: newWebClient(40*time.Second, collectorDomains(cfg, kind))}
	now := time.Now().UTC()
	data := &snapshot{Version: 3, SourceSet: cfg.fingerprint(kind), Source: kind,
		From: now.AddDate(0, -1, 0).Format(time.DateOnly), Through: now.Format(time.DateOnly), ObservedAt: now.Format(time.RFC3339)}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	t.Cleanup(cancel)
	t.Cleanup(func() {
		summarizeCoverage(data)
		if err := atomicSourceJSON(filepath.Join(dir, "snapshot.json"), data); err != nil {
			t.Error(err)
		}
		t.Logf("proof=%s records=%d requests=%d", dir, len(data.Records), s.requests)
	})
	return s, data, ctx
}

func TestLiveWebParsers(t *testing.T) {
	s, data, ctx := liveSourceService(t, "web")
	panel := []string{"MIT AI", "MIT Energy", "MIT Materials", "Google Research", "Hugging Face",
		"NASA features", "NASA news releases"}
	for _, source := range s.config.Web {
		if !slices.Contains(panel, source.Name) {
			continue
		}
		t.Run(source.Name, func(t *testing.T) {
			start := len(data.Records)
			if source.Kind == kindWordPress {
				window := period{Start: data.From, End: time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly)}
				c := coverage{Source: source.Name, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt}
				received := 0
				s.wordpressPage(ctx, source, window, 1, data, &c, &received)
				data.Coverage = append(data.Coverage, c)
			} else {
				source.Pages = 2
				s.feedHistory(ctx, source, data, map[string]record{})
			}
			rows := data.Records[start:]
			withText := 0
			for _, row := range rows {
				if row.TextStatus == articleText {
					withText++
					verifyLiveRecord(t, row)
				}
			}
			if withText == 0 {
				t.Fatalf("no real article text; coverage=%+v", data.Coverage[len(data.Coverage)-1])
			}
			t.Logf("records=%d with_text=%d sample=%s", len(rows), withText, rows[0].URL)
		})
	}
	liveExport(t, s, data, "web")
}

func TestLivePatentParsers(t *testing.T) {
	s, data, ctx := liveSourceService(t, kindPatents)
	t.Run("EPO", func(t *testing.T) {
		s.epHistory(ctx, data)
		if len(data.Records) == 0 {
			t.Fatalf("no EP patents; coverage=%+v", data.Coverage)
		}
		for _, row := range data.Records {
			verifyLiveRecord(t, row)
			if row.FullText == nil || len(row.FullText.Claims) == 0 {
				t.Fatalf("patent %s has no claims", row.ID)
			}
			t.Logf("id=%s publication=%s claims=%d sections=%d bytes=%d", row.ID, row.Date,
				len(row.FullText.Claims), len(row.FullText.Sections), len(row.Text))
		}
	})
	t.Run("USPTO", func(t *testing.T) {
		root, err := s.fetch(ctx, gazette, data)
		if err != nil {
			t.Fatal(err)
		}
		available, err := issues(root, data.From, data.Through)
		if err != nil {
			t.Fatal(err)
		}
		issue, _, err := s.resolvePatentIssue(ctx, available[0], data)
		if err != nil {
			t.Fatal(err)
		}
		address := issue.URL + "/OG/patentee/alphaA.html"
		body, err := s.fetch(ctx, address, data)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := patentRows(body, issue)
		if err != nil || len(rows) == 0 {
			t.Fatalf("no US patents: %v", err)
		}
		index := slices.IndexFunc(rows, func(r record) bool { return r.Title != "" })
		if index < 0 {
			t.Fatal("index has no unambiguous titles")
		}
		row := rows[index]
		body, err = s.fetch(ctx, row.URL, data)
		if err != nil {
			t.Fatal(err)
		}
		row, err = patentDocument(body, row, data.ObservedAt)
		if err != nil {
			t.Fatal(err)
		}
		row.Provenance = []provenance{{URL: row.URL, SHA256: hashBytes(body)}}
		data.Records = append(data.Records, row)
		verifyLiveRecord(t, row)
		if row.TextStatus != "representative_claim" {
			t.Fatalf("no representative claim: %+v", row)
		}
		t.Logf("id=%s publication=%s filing=%s bytes=%d", row.ID, row.Date, row.Filing, len(row.Text))
	})
	liveExport(t, s, data, "patent")
}

func verifyLiveRecord(t *testing.T, row record) {
	t.Helper()
	if row.ID == "" || row.Title == "" || sourceDay(row.Date) == "" || len(row.Text) < 80 ||
		len(row.Provenance) == 0 || row.ContentHash != hashBytes([]byte(row.Text)) {
		t.Fatalf("invalid real record: id=%s title=%q date=%s text=%d provenance=%d",
			row.ID, row.Title, row.Date, len(row.Text), len(row.Provenance))
	}
	size := len([]rune(row.Text))
	for _, anchor := range row.Anchors {
		if anchor.Path == "" || anchor.Start < 0 || anchor.End <= anchor.Start || anchor.End > size {
			t.Fatalf("invalid anchor: %+v", anchor)
		}
	}
	if row.FullText != nil {
		for _, claim := range row.FullText.Claims {
			if claim.Start < 0 || claim.End <= claim.Start || claim.End > size {
				t.Fatalf("invalid claim: %+v", claim)
			}
		}
	}
}

func liveExport(t *testing.T, s *service, data *snapshot, kind string) {
	t.Helper()
	index := slices.IndexFunc(data.Records, func(r record) bool {
		return len(r.Text) > 80 && r.Date >= data.From && r.Date <= data.Through
	})
	if index < 0 {
		t.Error("no dated content available for export")
		return
	}
	query := "*"
	q := url.Values{"query": {query}, "from": {data.From}, "through": {data.Through}, "limit": {"100"}}
	s.data = data
	w := httptest.NewRecorder()
	s.exportRecords(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/records?"+q.Encode(), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("export HTTP %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Records []record `json:"records"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Records) == 0 {
		t.Fatalf("export: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, kind+"-export.json"), w.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s export: %d records, query=%q", kind, len(response.Records), query)
}
