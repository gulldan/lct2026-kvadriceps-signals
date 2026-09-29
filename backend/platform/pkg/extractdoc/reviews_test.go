package extractdoc_test

import (
	"testing"

	"github.com/example/rag-mvp/pkg/extractdoc"
)

func TestReviewFingerprintIsStableForObjectValues(t *testing.T) {
	review := extractdoc.Review{Operation: extractdoc.OpSetRole, AuthorID: "expert",
		PreviousValue: map[string]any{"role": "background", "mention_id": "m-1", "scope": "s-1"},
		NewValue:      map[string]any{"role": "proposed_contribution", "mention_id": "m-1", "scope": "s-1"}}
	want := review.Fingerprint()
	for range 100 {
		if review.Fingerprint() != want {
			t.Fatal("object iteration order changed the identity of the same correction")
		}
	}
}
