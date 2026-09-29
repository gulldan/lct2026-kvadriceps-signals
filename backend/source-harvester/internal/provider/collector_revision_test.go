package provider

import (
	"encoding/json/jsontext"
	"strings"
	"testing"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
)

func TestCollectorFactualRevisionPreservesCorrectionsWithoutObservationDuplicates(t *testing.T) {
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	scope := Scope{Query: "hydrogen", From: "2026-09-01", Through: "2026-09-11", PageSize: 1}
	row := collectorRecord{ID: "USD1145480", Title: "Portable hydrogen water bottle", Text: "The ornamental design.",
		TextStatus: "representative_claim", Date: "2026-09-01", DateBasis: "USPTO issue date",
		Filing: "2025-12-09", URL: "https://patentsgazette.uspto.gov/design", FamilyID: "application:US30036840",
		AvailableAt: "2026-09-10T12:00:00Z", ObservedAt: "2026-09-10T12:00:00Z"}
	raw := jsontext.Value(`{"application":"US30036840",
		"provenance":[{"url":"https://example.org/source","sha256":"abc"}],
		"observed_at":"2026-09-10T12:00:00Z","available_at":"2026-09-10T12:00:00Z",
		"parser_version":"gazette-primary-v1"}`)
	before, err := collectorDocument("patent", row, raw, scope, at)
	if err != nil {
		t.Fatal(err)
	}
	key := func(r Record) string {
		return harvest.DocumentKey("owner", harvest.Prepared{Source: r.Source, MIME: "text/plain"})
	}
	for name, change := range map[string]func(*collectorRecord){
		"priority": func(r *collectorRecord) { r.Priority = "2025-10-30" },
		"filing":   func(r *collectorRecord) { r.Filing = "2025-12-08" },
		"family":   func(r *collectorRecord) { r.FamilyID = "us-priority-set:confirmed" },
	} {
		t.Run(name, func(t *testing.T) {
			corrected := row
			change(&corrected)
			after, changeErr := collectorDocument("patent", corrected, raw, scope, at)
			if changeErr != nil {
				t.Fatal(changeErr)
			}
			if before.Source.ContentHash != after.Source.ContentHash || key(before) == key(after) {
				t.Fatal("corrected fact was swallowed by unchanged-content deduplication")
			}
		})
	}
	// Дополнительные первичные поля живут в неизменном манифесте без своей колонки Source; их исправлениям нужна ревизия.
	for _, fact := range []string{
		`,"priority_ids":["CN202530639486.2"]}`,
		`,"original_url":"https://example.org/verified-original"}`,
	} {
		additional := jsontext.Value(strings.TrimSuffix(string(raw), "}") + fact)
		after, rawErr := collectorDocument("patent", row, additional, scope, at)
		if rawErr != nil || key(before) == key(after) {
			t.Fatalf("raw primary fact ignored: %v", rawErr)
		}
	}
	row.AvailableAt, row.ObservedAt = "2026-09-10T13:00:00Z", "2026-09-11T00:30:00Z"
	reordered := jsontext.Value(`{"parser_version":"gazette-primary-v2","available_at":"2026-09-10T13:00:00Z",
		"observed_at":"2026-09-11T00:30:00Z",
		"provenance":[{"sha256":"changed-feed-response","url":"https://example.org/source?next=page"}],
		"application":"US30036840"}`)
	repeat, err := collectorDocument("patent", row, reordered, scope, at.Add(time.Hour))
	if err != nil || key(before) != key(repeat) {
		t.Fatalf("observation, response provenance, parser upgrade or JSON order created a duplicate: %v", err)
	}
}
