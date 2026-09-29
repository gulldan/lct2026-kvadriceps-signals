package provider

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordedCollectorExports(t *testing.T) {
	dir := os.Getenv("TR_SOURCE_PROOF_DIR")
	if dir == "" {
		t.Skip("set TR_SOURCE_PROOF_DIR to a completed live parser proof")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*", "*-export.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no collector exports in %s: %v", dir, err)
	}
	for _, filename := range files {
		t.Run(filepath.Base(filepath.Dir(filename)), func(t *testing.T) {
			raw, readErr := os.ReadFile(filename)
			if readErr != nil || len(raw) > maxResponseBytes {
				t.Fatalf("read export: %v", readErr)
			}
			var page struct {
				Records []jsontext.Value `json:"records"`
			}
			if decodeErr := json.Unmarshal(raw, &page); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if len(page.Records) == 0 {
				t.Fatal("empty export is not a successful integration check")
			}
			kind := strings.TrimSuffix(filepath.Base(filename), "-export.json")
			if kind != providerPatent && kind != "web" {
				t.Fatalf("unknown export kind: %s", kind)
			}
			withText := 0
			for _, item := range page.Records {
				var row collectorRecord
				if decodeErr := json.Unmarshal(item, &row); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				scope := Scope{Query: "verification", From: "1900-01-01",
					Through: time.Now().UTC().Format(time.DateOnly), PageSize: 100}
				mapped, mapErr := collectorDocument(kind, row, item, scope, time.Now().UTC())
				if mapErr != nil {
					t.Fatal(mapErr)
				}
				if mapped.Source.ContentHash != digest([]byte(mapped.Text)) ||
					len(mapped.Source.Anchors) != len(row.Anchors)+liveClaimCount(row) {
					t.Fatalf("content or locators lost while importing %s", row.ID)
				}
				if row.Text != "" {
					withText++
					if len(mapped.Text) <= len(row.Title) {
						t.Fatal("saved article text disappeared during import")
					}
				}
			}
			if withText == 0 {
				t.Fatal("export contained only metadata, no article or patent text")
			}
			t.Logf("provider=%s records=%d with_text=%d", kind, len(page.Records), withText)
		})
	}
}

func liveClaimCount(row collectorRecord) int {
	if row.FullText == nil {
		return 0
	}
	return len(row.FullText.Claims)
}
