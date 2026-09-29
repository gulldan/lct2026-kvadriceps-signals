package provider

import (
	"encoding/json/jsontext"
	"strings"
	"testing"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
)

func TestCollectorPatentIdentityAndMissingText(t *testing.T) {
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	scope := Scope{Query: "hydrogen", From: "2026-09-01", Through: "2026-09-11", PageSize: 1}
	row := collectorRecord{ID: "US12729112", Title: "Hydrogen process", Date: "2026-09-08",
		DateBasis: "USPTO issue", Filing: "2025-07-21", URL: "https://patentsgazette.uspto.gov/one",
		FamilyID: "application:US19275366", AvailableAt: "2026-09-10T12:00:00Z"}
	a, e := collectorDocument("patent", row, jsontext.Value(`{"verified":true}`), scope, at)
	if e != nil {
		t.Fatal(e)
	}
	if a.Source.Priority != nil || a.Source.Filing.Value != "2025-07-21" ||
		a.TextStatus != "metadata_only" || len(a.Limitations) == 0 ||
		!strings.HasPrefix(a.Source.CanonicalID, "patent-family:") {
		t.Fatalf("source semantics: %+v", a)
	}
	b, e := collectorDocument("patent", row, jsontext.Value(`{"verified":true}`), scope, at.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	firstKey := harvest.DocumentKey("owner", harvest.Prepared{Source: a.Source, MIME: "text/plain"})
	secondKey := harvest.DocumentKey("owner", harvest.Prepared{Source: b.Source, MIME: "text/plain"})
	if firstKey != secondKey {
		t.Fatal("repeat observation duplicated immutable document")
	}
	row.Priority, row.PriorityBasis = "2025-01-15", "USPTO Patent Public Search foreign priority claim"
	b, e = collectorDocument("patent", row, jsontext.Value(`{"verified":true}`), scope, at)
	if e != nil || b.Source.Priority.Basis != row.PriorityBasis {
		t.Fatalf("priority evidence was relabelled as a different primary source: %v", e)
	}
	row.PriorityBasis = ""
	b, e = collectorDocument("patent", row, jsontext.Value(`{"verified":true}`), scope, at)
	if e != nil || b.Source.Priority.Basis != "USPTO primary document priority claim" {
		t.Fatalf("legacy primary priority basis changed: %v", e)
	}
	row.Date = "2026-08-31"
	if _, e = collectorDocument("patent", row, nil, scope, at); e == nil {
		t.Fatal("outside date accepted")
	}
}
