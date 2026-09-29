package main

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestPrimaryPatentSeparatesDatesAndPreservesClaim(t *testing.T) {
	raw, e := os.ReadFile("testdata/uspto-US12729112.html")
	if e != nil {
		t.Fatal(e)
	}
	r, e := patentDocument(raw, record{ID: "US12729112", Date: "2026-09-08"}, "2026-09-11T00:00:00Z")
	if e != nil || r.Title != "Protonation approach to improve the green hydrogen production using aluminum alloy" ||
		r.Filing != "2025-07-21" || r.Priority != "" || r.Date != "2026-09-08" || r.FamilyID != "application:US19275366" ||
		r.TextStatus != "representative_claim" || !strings.Contains(r.Text, "aluminum-based alloy") {
		t.Fatalf("unreliable patent: %+v %v", r, e)
	}
	if _, e = patentDocument(raw, record{ID: "US1272911"}, "2026-09-11T00:00:00Z"); e == nil {
		t.Fatal("different patent number accepted")
	}
}
func TestPublisherContentDatesAndCanonicalLinks(t *testing.T) {
	if sourceDay("2026-09-08T23:30:00-04:00") != "2026-09-08" {
		t.Fatal("publisher calendar date was changed")
	}
	article := []byte(`<html><head><meta property="article:published_time" content="2026-09-08T10:00:00Z"><` +
		`link rel="canonical" href="https://lab.example/story"></head><body><article><` +
		`p>A hydrogen catalyst improves energy efficiency using a carefully controlled reaction and an independently described ` +
		`laboratory process.</p></article><script>alert('do not index')</script></body></html>`)
	r, e := articleDocument(article, record{ID: "x", URL: "https://lab.example/story?utm_source=feed", Date: "2026-09-08"},
		"2026-09-11T00:00:00Z")
	if e != nil || r.CanonicalURL != "https://lab.example/story" || r.TextStatus != "article_text" || strings.Contains(r.Text,
		"alert") {
		t.Fatalf("article: %+v %v", r, e)
	}
	if _, e = articleDocument(article, record{Date: "2026-09-07"}, "2026-09-11T00:00:00Z"); e == nil {
		t.Fatal("conflicting dates accepted")
	}
	rows := []record{{ID: "a", URL: "https://lab.example/a", Date: "2026-09-01", Text: "Exact reprint content"}, {ID: "b",
		URL: "https://lab.example/b", Date: "2026-09-02", Text: "Exact reprint content"}}
	linkDuplicates(rows)
	if rows[1].OriginalURL != rows[0].URL {
		t.Fatal("reprint not linked")
	}
}
func TestWordPressDatedArchiveAndProtectedContent(t *testing.T) {
	raw := []byte(`[{"id":1,"date_gmt":"2026-08-31T23:59:59","status":"publish",` +
		`"link":"https://lab.example/old","title":{"rendered":"Old"},` +
		`"content":{"rendered":"old"}},` +
		`{"id":2,"date_gmt":"2026-09-01T00:00:00","status":"publish",` +
		`"link":"https://lab.example/current","title":{"rendered":"Energy &amp; materials"},` +
		`"content":{"rendered":"<p>Private</p>","protected":true}}]`)
	rows, n, e := wordpressRows(raw, webSource{Name: "Lab"}, period{"2026-09-01", "2026-09-11"}, "2026-09-11T00:00:00Z")
	if e != nil || n != 2 || len(rows) != 1 || rows[0].Title != "Energy & materials" || rows[0].Text != "" ||
		rows[0].TextStatus != "metadata_only" {
		t.Fatalf("archive: %+v %d %v", rows, n, e)
	}
}
func TestExportPinnedCursorSurvivesObservationButRejectsFactChange(t *testing.T) {
	s := &service{data: &snapshot{SourceSet: "source-v1", ObservedAt: "2026-09-10T00:00:00Z", Records: []record{{ID: "a",
		Title: "Hydrogen A", Date: "2026-09-01"}, {ID: "b", Title: "Hydrogen B", Date: "2026-09-02"}}}}
	read := func(cursor string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/v1/records?limit=1&from=2026-09-01&through=2026-09-11&query=hydrogen&cursor="+cursor, nil)
		w := httptest.NewRecorder()
		s.exportRecords(w, r)
		return w
	}
	w := read("")
	var first struct {
		Next string `json:"next_cursor"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &first); e != nil || first.Next == "" {
		t.Fatal(w.Body.String(), e)
	}
	s.data.ObservedAt = "2026-09-11T00:00:00Z"
	s.data.Records[0].ObservedAt = s.data.ObservedAt
	s.data.Records[0].AvailableAt = s.data.ObservedAt
	s.data.Records[0].ParserVersion = "gazette-primary-v2"
	s.data.Records[0].Provenance = []provenance{{URL: "https://publisher.example/feed", SHA256: "new feed response"}}
	before, err := json.Marshal(s.data)
	if err != nil {
		t.Fatal(err)
	}
	if w = read(first.Next); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"b"`) {
		t.Fatal("observation reset cursor", w.Body.String())
	}
	after, err := json.Marshal(s.data)
	if err != nil || string(before) != string(after) {
		t.Fatal("cursor hashing mutated the source snapshot", err)
	}
	for _, change := range []struct {
		name  string
		apply func(*record)
	}{
		{"text", func(r *record) { r.Text = "new version" }},
		{"priority", func(r *record) { r.Priority = "2025-10-30" }},
		{"priority IDs", func(r *record) { r.PriorityIDs = []string{"CN202530639486.2"} }},
		{"family", func(r *record) { r.FamilyID = "us-priority-set:confirmed" }},
		{"filing", func(r *record) { r.Filing = "2025-12-09" }},
		{"original", func(r *record) { r.OriginalURL = "https://publisher.example/original" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			original := s.data.Records[0]
			t.Cleanup(func() { s.data.Records[0] = original })
			change.apply(&s.data.Records[0])
			if w = read(first.Next); w.Code != 409 {
				t.Fatal("changed facts silently advanced", w.Body.String())
			}
		})
	}
}
func TestCoverageConfigurationAndInitialReadiness(t *testing.T) {
	c := defaultConfig()
	a := c.fingerprint("web")
	c.RequestBudget++
	if a != c.fingerprint("web") {
		t.Fatal("request budget changed corpus")
	}
	c.Web[0].URL += "/changed"
	if a == c.fingerprint("web") {
		t.Fatal("source population change invisible")
	}
	s := &service{}
	w := httptest.NewRecorder()
	s.status(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/status", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestPrimaryPCTAndPriorityFamilyData(t *testing.T) {
	for _, id := range []string{"US12728615", "US12729124"} {
		raw, e := os.ReadFile("testdata/uspto-" + id + ".html")
		if e != nil {
			t.Fatal(e)
		}
		row, e := patentDocument(raw, record{ID: id, Date: "2026-09-08"}, "2026-09-11T00:00:00Z")
		if e != nil {
			t.Fatal(e)
		}
		if row.Application == "" || row.PCTFiling == "" || row.NationalStage == "" || row.Priority == "" ||
			len(row.PriorityIDs) == 0 || !strings.HasPrefix(row.FamilyID, "us-priority-set:") {
			t.Fatalf("lost primary dates or family basis %+v", row)
		}
		if row.Priority >= row.Filing || row.Filing >= row.NationalStage || row.NationalStage >= row.Date {
			t.Fatalf("conflated dates %+v", row)
		}
		if id == "US12729124" && len(row.PriorityIDs) != 3 {
			t.Fatalf("lost priority applications %+v", row.PriorityIDs)
		}
	}
}

func BenchmarkRecordsVersion(b *testing.B) {
	rows := make([]record, 50_000)
	for i := range rows {
		rows[i] = record{ID: strconv.Itoa(i), Title: "Hydrogen production", Text: strings.Repeat("Representative claim. ", 20),
			Date: "2026-09-01", ObservedAt: "2026-09-11T00:00:00Z"}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := recordsVersion("source-v1", rows); err != nil {
			b.Fatal(err)
		}
	}
}
