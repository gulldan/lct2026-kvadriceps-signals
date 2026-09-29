package provider

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

func TestLiveCrossrefParsers(t *testing.T) {
	if os.Getenv("TR_LIVE_SOURCES") != "1" {
		t.Skip("set TR_LIVE_SOURCES=1 for public source verification")
	}
	dir := t.TempDir()
	if root := os.Getenv("TR_SOURCE_PROOF_DIR"); root != "" {
		if err := os.MkdirAll(root, 0750); err != nil {
			t.Fatal(err)
		}
		var err error
		dir, err = os.MkdirTemp(root, "papers-")
		if err != nil {
			t.Fatal(err)
		}
	}
	c := NewCrossref(os.Getenv("PUBSEARCH_MAILTO"))
	for _, sample := range []struct{ name, query, workType string }{
		{"materials", "materials discovery", "journal-article"},
		{"ai-conferences", "artificial intelligence", "proceedings-article"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			scope := Scope{Query: sample.query, WorkType: sample.workType, PageSize: 5,
				From: "2023-01-01", Through: time.Now().UTC().Format(time.DateOnly)}
			page, err := c.Fetch(t.Context(), scope, "")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, sample.name+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err = page.Validate(scope.PageSize); err != nil || len(page.Records) == 0 {
				t.Fatalf("no valid scientific records: %v, rejected=%+v", err, page.Rejected)
			}
			abstracts, fullTexts := 0, 0
			for _, row := range page.Records {
				if err = row.Source.Validate(); err != nil || row.Source.ContentHash != digest([]byte(row.Text)) {
					t.Fatalf("invalid source or content hash: %v", err)
				}
				if err = sourcedoc.ValidateSections(row.Source.Sections, utf8.RuneCountInString(row.Text)); err != nil {
					t.Fatal(err)
				}
				if row.TextStatus == "full_text" {
					fullTexts++
				}
				if row.TextStatus == "abstract_only" {
					abstracts++
					if len(row.Source.Sections) != 1 {
						t.Fatal("scientific abstract lost its section")
					}
				}
			}
			if sample.name == "materials" && abstracts+fullTexts == 0 {
				t.Fatal("metadata succeeded, but no scientific abstract or full text was obtained")
			}
			t.Logf("records=%d abstracts=%d full_texts=%d proof=%s",
				len(page.Records), abstracts, fullTexts, filepath.Join(dir, sample.name+".json"))
		})
	}
}
