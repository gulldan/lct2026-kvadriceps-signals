package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOPSRecordsMapDOCDBBiblioSearch(t *testing.T) {
	raw, err := os.ReadFile("testdata/ops-biblio-search.json")
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := opsRecords(raw, "2026-09-15T00:00:00Z")
	if err != nil || total != 276 || len(rows) != 3 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
	row := rows[0]
	if row.ID != "CN223858181U" || row.KindCode != "U" || row.Date != "2026-01-30" || row.FamilyID != "docdb:98552420" ||
		row.FamilyBasis != opsFamilyBasis || row.Source != opsSource ||
		!strings.HasPrefix(row.URL, "https://worldwide.espacenet.com/") {
		t.Fatalf("identity: %+v", row)
	}
	if row.Title == "" || row.Text == "" || row.TextStatus != opsAbstractStatus || row.Language != "en" ||
		row.FullText == nil || row.FullText.Sections[0].Name != "abstract" || len(row.FullText.CPC) == 0 {
		t.Fatalf("title/abstract: title=%q text=%d status=%s lang=%s full=%+v", row.Title, len(row.Text), row.TextStatus,
			row.Language, row.FullText)
	}
	if row.Filing == "" || row.Application == "" || row.FilingBasis == "" {
		t.Fatalf("application: %+v", row)
	}
	if colour, perMinute := opsThrottle(http.Header{"X-Throttling-Control": {
		"busy (images=green:100, inpadoc=green:45, other=green:1000, retrieval=green:100, search=yellow:8)"}}); colour != "yellow" ||
		perMinute != 8 {
		t.Fatalf("throttle: %s %d", colour, perMinute)
	}
	if got := opsCQL(`solid state "batter`, period{Start: "2026-01-01", End: "2026-02-01"}); got !=
		`ta="solid state batter*" and pd within "20260101 20260131"` {
		t.Fatalf("cql: %s", got)
	}
}

func TestOPSHistoryAuthenticatesPacesAndStopsOnRejection(t *testing.T) {
	fixture, err := os.ReadFile("testdata/ops-biblio-search.json")
	if err != nil {
		t.Fatal(err)
	}
	searches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/accesstoken":
			if user, pass, ok := r.BasicAuth(); !ok || user != "key" || pass != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":"1199","token_type":"Bearer"}`))
		case r.URL.Path == "/rest-services/published-data/search/biblio":
			searches++
			if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Accept") != "application/json" ||
				!strings.Contains(r.URL.Query().Get("q"), `ta="solid state batter*"`) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("X-Throttling-Control",
				"idle (images=green:200, inpadoc=green:60, other=green:1000, retrieval=green:200, search=green:1000)")
			if searches > 1 {
				w.Header().Set("X-Rejection-Reason", "RegisteredQuotaPerWeek exceeded")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write(fixture)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("EPO_OPS_KEY", "key")
	t.Setenv("EPO_OPS_SECRET", "secret")
	s := &service{kind: kindPatents, dir: t.TempDir(), http: server.Client(), opsURL: server.URL,
		config: sourceConfig{HistoryMonths: 2, RequestBudget: 20, OPSDocuments: 500, PatentTerms: []string{"solid state batter"}}}
	data := snapshot{ObservedAt: "2026-09-15T00:00:00Z", From: "2026-08-01", Through: "2026-09-15", Records: []record{},
		Pages: []provenance{}, Limits: []string{}}
	s.opsHistory(context.Background(), &data)
	if len(data.Records) != 3 || len(data.Coverage) != 1 || data.Coverage[0].Source != opsSource {
		t.Fatalf("records=%d coverage=%+v", len(data.Records), data.Coverage)
	}
	c := data.Coverage[0]
	if c.Exhausted || !strings.Contains(c.StopReason, "RegisteredQuotaPerWeek") || c.Cursor == "" || searches != 2 {
		t.Fatalf("rejection must stop the sweep at a cursor: %+v searches=%d", c, searches)
	}
	if len(data.Pages) != 1 || data.Records[0].Provenance[0].SHA256 != data.Pages[0].SHA256 {
		t.Fatalf("provenance: pages=%d records=%+v", len(data.Pages), data.Records[0].Provenance)
	}
	if s.ops.token != "tok" || time.Until(s.ops.expires) < 15*time.Minute {
		t.Fatalf("token cache: %+v", s.ops)
	}
}
