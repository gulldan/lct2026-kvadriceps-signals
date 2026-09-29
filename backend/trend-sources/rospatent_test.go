package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRospatentHistoryReadsTermMonthsWithBearerKey(t *testing.T) {
	fixture, err := os.ReadFile("testdata/rospatent-search.json")
	if err != nil {
		t.Fatal(err)
	}
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search" || r.Header.Get("Authorization") != "Bearer rp-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "Authorization error"}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		// Отказ по одному термину не останавливает обход: следующий термин читается.
		if body["q"] == rospatentQueryTerm("rejected") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	t.Setenv("ROSPATENT_API_KEY", "rp-key")
	s := &service{kind: kindPatents, dir: t.TempDir(), http: server.Client(), rospatentURL: server.URL,
		config: sourceConfig{HistoryMonths: 1, RequestBudget: 20, RUDocuments: 50,
			PatentTerms: []string{"rejected", "solid state batter"}}}
	data := snapshot{ObservedAt: "2026-09-15T00:00:00Z", From: "2026-01-01", Through: "2026-01-31", Records: []record{},
		Pages: []provenance{}, Limits: []string{}}
	s.rospatentHistory(context.Background(), &data)
	if len(bodies) != 1 || bodies[0]["q"] != "solid state batter" || fmt.Sprint(bodies[0]["datasets"]) != "["+rospatentDataset+"]" {
		t.Fatalf("query bodies: %+v", bodies)
	}
	if got := fmt.Sprint(bodies[0]["filter"]); !strings.Contains(got, "gte:20260101") || !strings.Contains(got, "lte:20260131") {
		t.Fatalf("month filter: %s", got)
	}
	if len(data.Records) != 2 || len(data.Coverage) != 1 || data.Coverage[0].Exhausted ||
		!strings.Contains(strings.Join(data.Coverage[0].Limitations, " "), "term skipped: rejected") {
		t.Fatalf("records=%d coverage=%+v", len(data.Records), data.Coverage)
	}
	row := data.Records[0]
	if row.ID != "RU2359130C1" || row.Title != "SOLID STATE BATTERY" || row.Language != "en" || row.Date != "2026-01-20" ||
		row.Filing != "2025-03-11" || row.Priority != "2025-03-11" || row.FamilyID != "docdb:98552420" ||
		row.TextStatus != opsAbstractStatus || row.FullText == nil || len(row.FullText.CPC) != 2 ||
		row.Text != "The invention relates to solid state batteries with a sulfide electrolyte." ||
		row.Provenance[0].Method != http.MethodPost {
		t.Fatalf("english record: %+v", row)
	}
	if other := data.Records[1]; other.ID != "RU0000134694U1" || other.Language != "ru" || other.Text != "" ||
		other.TextStatus != metadataOnlyText || other.FamilyID != "application:RU2025112345/07" {
		t.Fatalf("russian record: %+v", other)
	}
}
