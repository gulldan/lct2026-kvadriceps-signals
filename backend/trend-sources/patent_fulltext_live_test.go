package main

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPatentFullTextLive(t *testing.T) {
	guids := strings.Fields(os.Getenv("TR_PATENT_FULLTEXT_GUIDS"))
	if len(guids) == 0 {
		t.Skip("set TR_PATENT_FULLTEXT_GUIDS to run the live USPTO full-text check")
	}
	s := &service{kind: kindPatents, dir: t.TempDir(), config: defaultConfig(),
		http: &http.Client{Timeout: 90 * time.Second}}
	client := newPatentSearchClient(s)
	for _, guid := range guids {
		t.Run(guid, func(t *testing.T) {
			page, body, err := client.highlight(t.Context(), guid)
			if err != nil {
				t.Fatal(err)
			}
			text, sections, claims, missing := patentSections(page)
			if len(sections) < 3 || len(claims) == 0 {
				t.Fatalf("%s: %d sections, %d claims, missing %v", guid, len(sections), len(claims), missing)
			}
			for _, claim := range claims {
				quote := text[claim.Start:claim.End]
				if strings.TrimSpace(quote) == "" || !strings.Contains(text, quote) {
					t.Fatalf("claim %d does not reproduce from its locator", claim.Number)
				}
			}
			// Нумерация пунктов сплошная: пропущенный пункт разорвал бы цепочку.
			if claims[len(claims)-1].Number != len(claims) {
				t.Fatalf("%s declares %d claims but %d were parsed",
					guid, claims[len(claims)-1].Number, len(claims))
			}
			independent := 0
			for _, claim := range claims {
				if !claim.Dependent {
					independent++
				}
			}
			if independent == 0 {
				t.Fatalf("%s: every claim was read as dependent", guid)
			}
			row, applyErr := applyPatentFullText(record{ID: patentIDFromGUID(guid), KindCode: kindFromGUID(guid),
				ParserVersion: patentMetadataVersion}, page,
				provenance{URL: client.base, SHA256: hashBytes(body)}, time.Now().UTC().Format(time.RFC3339))
			if applyErr != nil {
				t.Fatal(applyErr)
			}
			t.Log(guid, "sections", len(sections), "claims", len(claims), "independent", independent,
				"missing", missing, "bytes", strconv.Itoa(len(body)), "language", row.Language)
		})
	}
}

func patentIDFromGUID(guid string) string {
	parts := strings.Split(guid, "-")
	if len(parts) != 3 {
		return ""
	}
	return "US" + parts[1]
}

func kindFromGUID(guid string) string {
	parts := strings.Split(guid, "-")
	if len(parts) != 3 {
		return ""
	}
	return parts[2]
}
