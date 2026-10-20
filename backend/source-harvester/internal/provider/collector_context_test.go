package provider

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

func TestCollectorInvestorScopeAndProvenance(t *testing.T) {
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	day := at.Format(time.DateOnly)
	row := collectorRecord{ID: "https://example.org/thesis", URL: "https://example.org/thesis",
		Title: "Industrial heat investment thesis", Text: "Public research directions, without a funding commitment.",
		TextStatus: "article_text", Genre: sourcedoc.GenreInvestmentThesis, ObservedAt: at.Format(time.RFC3339)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/records" || q.Get("query") != "*" {
			t.Errorf("wrong collector request: %s", r.URL)
		}
		out := row
		if q.Get("genre") == "" {
			if q.Has("include_undated") {
				t.Error("ordinary scope received undated opt-in")
			}
			out.Date, out.Genre = day, sourcedoc.GenreInvestorCommentary
		} else if q.Get("genre") != sourcedoc.GenreInvestmentThesis || q.Get("include_undated") != "1" {
			t.Errorf("investor scope filter lost: %s", r.URL)
		}
		if err := json.MarshalWrite(w, map[string]any{
			"records": []collectorRecord{out}, "exhausted": true, "total": 1, "source_set": "investor-fixture",
		}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("WEB_COLLECTOR_URL", server.URL)
	adapter, err := NewCollector(providerWeb)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Query: "*", From: day, Through: day, PageSize: 1, WorkType: sourcedoc.GenreInvestmentThesis}
	page, err := adapter.Fetch(t.Context(), scope, "")
	if err != nil || len(page.Records) != 1 || len(page.Rejected) != 0 {
		t.Fatalf("investor fetch: %+v, %v", page, err)
	}
	raw, err := json.Marshal(page.Records[0].Source)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := sourcedoc.Decode(raw)
	if err != nil || decoded.Genre != sourcedoc.GenreInvestmentThesis || decoded.Publication != nil ||
		!decoded.ObservedAt.Equal(at) || decoded.Provider != providerWeb || decoded.Kind != "analysis" {
		t.Fatalf("HTTP to source metadata lost context: %s (%v)", raw, err)
	}
	for _, other := range []string{providerPatent, providerArXiv, "crossref", "github"} {
		if validationErr := scope.ValidateFor(other); validationErr == nil {
			t.Fatalf("investor work type accepted for %s", other)
		}
	}
	for name, change := range map[string]func(*collectorRecord){
		"wrong genre":         func(r *collectorRecord) { r.Genre = sourcedoc.GenreInvestorCommentary },
		"missing genre":       func(r *collectorRecord) { r.Genre = "" },
		"invalid date":        func(r *collectorRecord) { r.Date = " " },
		"missing observation": func(r *collectorRecord) { r.ObservedAt = "" },
		"future observation":  func(r *collectorRecord) { r.ObservedAt = at.Add(48 * time.Hour).Format(time.RFC3339) },
		"outside observation": func(r *collectorRecord) { r.ObservedAt = at.Add(-48 * time.Hour).Format(time.RFC3339) },
	} {
		bad := row
		change(&bad)
		if _, contextErr := collectorDocument(providerWeb, bad, jsontext.Value(`{}`), scope, time.Now()); contextErr == nil {
			t.Errorf("accepted %s", name)
		}
	}
	scope.WorkType = "journal-article"
	if _, contextErr := collectorDocument(providerWeb, row, jsontext.Value(`{}`), scope, time.Now()); contextErr == nil {
		t.Error("ordinary scope accepted undated investor page")
	}
	page, err = adapter.Fetch(t.Context(), scope, "")
	if err != nil || len(page.Records) != 1 || page.Records[0].Source.Genre != sourcedoc.GenreInvestorCommentary ||
		page.Records[0].Source.Publication == nil || page.Records[0].Source.Publication.Value != day {
		t.Fatalf("dated commentary lost publication or genre: %+v, %v", page, err)
	}
}
