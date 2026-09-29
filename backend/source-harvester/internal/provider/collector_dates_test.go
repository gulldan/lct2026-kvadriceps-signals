package provider

import (
	"testing"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

func TestCollectorPublicationIsNotAlwaysAGrant(t *testing.T) {
	for _, tc := range []struct {
		id, kind, basis string
		grant           bool
	}{
		{"CN122065940A", "A", "Google Patents publicationDate", false},
		{"CN122065940B", "B", "Google Patents publicationDate", true},
		{"CN224265727U", "U", "Google Patents publicationDate", true},
		{"EP2151037B1", "B1", "EPO publication date (ST.32 B140)", true},
		{"EP2151037A1", "A1", "EPO publication date (ST.32 B140)", false},
		{"US12729112", "", "USPTO issue date", true},
		{"US12729111", "", "USPTO issue cover 2026-09-08; directory label 2026-09-02", true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			source := sourcedoc.Source{Kind: providerPatent, Publication: &sourcedoc.Date{Value: "2026-05-19", Precision: precisionDay}}
			collectorPatentDates(&source, collectorRecord{ID: tc.id, KindCode: tc.kind, DateBasis: tc.basis})
			if (source.Grant != nil) != tc.grant {
				t.Fatalf("grant=%v for publication kind %s", source.Grant, tc.kind)
			}
		})
	}
}
