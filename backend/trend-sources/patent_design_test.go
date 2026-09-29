package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestDesignPatentOriginalNumberDatesAndPriority(t *testing.T) {
	// Сохранённый первичный документ USPTO, а не приближение:
	// https://patentsgazette.uspto.gov/week35/OG/html/1550-1/USD1145480-20260901.html
	// SHA256: 38a911f8a06ac9ad1a070100e7149dc081edd48d588637d4d9850e9f2a530545.
	raw, err := os.ReadFile("testdata/uspto-USD1145480.html")
	if err != nil {
		t.Fatal(err)
	}
	row, err := patentDocument(raw, record{ID: "USD1145480", Date: "2026-09-01"}, "2026-09-11T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.Title != "Portable hydrogen water bottle" || row.Date != "2026-09-01" ||
		row.Filing != "2025-12-09" || row.Priority != "2025-10-30" {
		t.Fatalf("design title or distinct dates lost: %+v", row)
	}
	if row.Application != "US30036840" || !slices.Equal(row.PriorityIDs, []string{"CN202530639486.2"}) ||
		!strings.HasPrefix(row.FamilyID, "us-priority-set:") || len(row.PriorityClaims) != 1 {
		t.Fatalf("explicit application or priority claim lost: %+v", row)
	}
	if row.TextStatus != "representative_claim" ||
		row.Text != "The ornamental design for a portable hydrogen water bottle as shown and described." {
		t.Fatalf("design claim was not preserved: %+v", row)
	}
	for _, id := range []string{"USD1145481", "US1145480"} {
		if _, err := patentDocument(raw, record{ID: id}, "2026-09-11T00:00:00Z"); err == nil {
			t.Fatalf("original design document accepted for %s", id)
		}
	}
	// Код вида S — только у образцов; для полезного патента без числового кода проверку не ослаблять.
	invalidUtility := []byte(strings.Replace(string(raw), "US D1,145,480 S", "US 1,145,480 S", 1))
	if _, err := patentDocument(invalidUtility, record{ID: "US1145480"}, "2026-09-11T00:00:00Z"); err == nil {
		t.Fatal("single-letter design kind accepted for a utility patent")
	}
}
