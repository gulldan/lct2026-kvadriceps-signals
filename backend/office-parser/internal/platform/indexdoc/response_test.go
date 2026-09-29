package indexdoc_test

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/example/office-parser/internal/platform/indexdoc"
	"github.com/example/office-parser/internal/platform/sourcedoc"
)

// Максимум элементов запроса должен помещаться в ответ вместе с разрешёнными метаданными.
func TestBatchResponseBudget(t *testing.T) {
	source := sourcedoc.Source{
		Version: 1, Kind: "paper", Provider: "manual", NativeID: "paper", CanonicalID: "paper",
		URL: "https://example.org/paper", ObservedAt: time.Now().UTC(),
		Revision: "v1", HashAlgorithm: "sha256", ContentHash: strings.Repeat("a", 64), AccessScope: "public",
	}
	for n := range 48 {
		source.Anchors = append(source.Anchors, sourcedoc.Anchor{Path: "/" + strings.Repeat("a", 511), Start: n, End: n + 1})
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sourcedoc.Decode(raw); err != nil {
		t.Fatal(err)
	}
	states := indexdoc.States{}
	for range 256 {
		states.Documents = append(states.Documents, indexdoc.State{DocumentID: "doc", Allowed: true, Source: &source})
	}
	raw, err = json.Marshal(states)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= indexdoc.MaxRequestBytes {
		t.Fatal("fixture must distinguish request and response budgets")
	}
	got, err := indexdoc.DecodeResponse[indexdoc.States](string(raw))
	if err != nil || len(got.Documents) != 256 {
		t.Fatalf("valid batch rejected: %v", err)
	}
	if _, err = indexdoc.DecodeResponse[indexdoc.States](strings.Repeat(" ", indexdoc.MaxResponseBytes+1)); err == nil {
		t.Fatal("oversized response accepted")
	}
}
