package provider

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

const jatsExample = `<article xml:lang="en"><front><article-meta>
<article-id pub-id-type="doi">10.1234/paper</article-id>
<abstract><p>Нейро<italic>сеть</italic> and H<sub>2</sub>.</p></abstract>
</article-meta></front><body><sec><title>Introduction</title><p>Scientific evidence.</p>
<sec><title>Nested study</title><p>Preserved nested content.</p></sec></sec>
<sec><title>Materials and methods</title><p>A &amp; B.</p><table-wrap><table>
<tr><td>Model A</td><td>92%</td></tr></table></table-wrap></sec></body>
<back><ref-list><ref>Prior work.</ref></ref-list></back></article>`

func TestEuropePMCFullTextAndIncrementalIdentity(t *testing.T) {
	for _, scenario := range []string{"full", "absent", "closed", "mismatch", "malformed", "retry"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Accept") == "application/json" && strings.HasSuffix(r.URL.Path, "/fullTextXML") {
					w.WriteHeader(http.StatusNotAcceptable)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/search") {
					if r.URL.Query().Get("query") != `DOI:"10.1234/paper"` {
						t.Error("lookup must use exact DOI")
					}
					if scenario == "absent" {
						fmt.Fprint(w, `{"hitCount":0,"resultList":{"result":[]}}`)
						return
					}
					open := "Y"
					if scenario == "closed" {
						open = "N"
					}
					fmt.Fprintf(w, `{"hitCount":1,"resultList":{"result":[{"doi":"10.1234/paper","pmcid":"PMC123",
"isOpenAccess":%q,"abstractText":"<h4>Methods</h4>Abstract supplied by Europe PMC."}]}}`, open)
					return
				}
				if r.URL.Path != "/PMC123/fullTextXML" {
					t.Errorf("unexpected request %s", r.URL.Path)
				}
				switch scenario {
				case "retry":
					w.Header().Set("Retry-After", "120")
					w.WriteHeader(http.StatusServiceUnavailable)
				case "mismatch":
					fmt.Fprint(w, strings.ReplaceAll(jatsExample, "10.1234/paper", "10.1234/other"))
				case "malformed":
					fmt.Fprint(w, `<article><body>`)
				default:
					fmt.Fprint(w, jatsExample)
				}
			}))
			t.Cleanup(server.Close)
			e := newEuropePMC()
			e.base = server.URL
			e.fetcher.interval = 0
			raw := crItem("paper", "[[2025,3,1]]")
			record, _, err := crossrefRecord([]byte(raw), testObserved(), testScope())
			if err != nil {
				t.Fatal(err)
			}
			err = e.enrich(t.Context(), &record)
			if scenario == "retry" {
				if _, ok := errors.AsType[*RequestError](err); !ok || requests != 2 {
					t.Fatalf("transient failure became a checkpoint: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "abstract_only"
			if scenario == "full" {
				want = "full_text"
			}
			if scenario == "absent" {
				want = "title_only"
			}
			if record.TextStatus != want {
				t.Fatalf("got %s, want %s", record.TextStatus, want)
			}
			if record.Source.ContentHash != digest([]byte(record.Text)) {
				t.Fatal("content hash mismatch")
			}
			if scenario == "full" {
				verifyJATSPaper(t, record)
				if !strings.Contains(record.Text, "Нейросеть and H2.") || !strings.Contains(record.Text, "Model A 92%") {
					t.Fatal("lost inline science text or table boundaries")
				}
			}
			var evidence paperEnrichment
			if json.Unmarshal(record.Raw, &evidence) != nil || string(evidence.Crossref) != raw {
				t.Fatal("lost original Crossref bytes")
			}
			if evidence.XML != "" && evidence.SHA256 != digest([]byte(evidence.XML)) {
				t.Fatal("lost XML provenance")
			}
		})
	}
	first, _, err := crossrefRecord([]byte(crItem("paper", "[[2025,3,1]]")), testObserved(), testScope())
	if err != nil {
		t.Fatal(err)
	}
	changedIndex := strings.Replace(crItem("paper", "[[2025,3,1]]"), `"type":`,
		`"indexed":{"timestamp":999},"is-referenced-by-count":17,"type":`, 1)
	second, _, err := crossrefRecord([]byte(changedIndex), testObserved().Add(time.Hour), testScope())
	if err != nil || first.Source.Revision != second.Source.Revision {
		t.Fatal("index/citation refresh created another document", err)
	}
	if paperRevision(first.Text+" amended", first.Source.Publication) == first.Source.Revision {
		t.Fatal("real text update was hidden")
	}
}

func verifyJATSPaper(t *testing.T, record Record) {
	t.Helper()
	runes := []rune(record.Text)
	if err := sourcedoc.ValidateSections(record.Source.Sections, len(runes)); err != nil {
		t.Fatal(err)
	}
	if err := sourcedoc.ValidateAnchors(record.Source.Anchors, len(runes)); err != nil {
		t.Fatal(err)
	}
	if len(record.Source.Sections) < 2 || len(record.Source.Anchors) != len(record.Source.Sections) {
		t.Fatal("full text lost its structure")
	}
	for i, section := range record.Source.Sections {
		anchor := record.Source.Anchors[i]
		if section.Start != anchor.Start || section.End != anchor.End ||
			strings.TrimSpace(string(runes[section.Start:section.End])) == "" {
			t.Fatal("source locator does not address saved Unicode text")
		}
	}
}

func TestLiveEuropePMCFullText(t *testing.T) {
	if os.Getenv("TR_LIVE_SOURCES") != "1" {
		t.Skip("set TR_LIVE_SOURCES=1 for public source verification")
	}
	// Статья в открытом доступе: сбой полного текста провалит проверку, а не сойдёт за удачный поиск метаданных.
	const doi = "10.3389/fdata.2026.1884673"
	c := NewCrossref("")
	scope := Scope{Query: "Benchmarking retrieval augmented generation LLMs for Arabic noise robustness",
		From: "2026-08-01", Through: "2026-09-13", WorkType: "journal-article", PageSize: 1}
	page, err := c.Fetch(t.Context(), scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].Source.NativeID != doi || page.Records[0].TextStatus != "full_text" {
		t.Fatalf("full-text path failed: %+v", page.Rejected)
	}
	record := page.Records[0]
	verifyJATSPaper(t, record)
	if utf8.RuneCountInString(record.Text) < 30000 {
		t.Fatal("full article was reduced to an abstract")
	}
	if root := os.Getenv("TR_SOURCE_PROOF_DIR"); root != "" {
		if err = os.MkdirAll(root, 0750); err != nil {
			t.Fatal(err)
		}
		raw, encodeErr := json.Marshal(page)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if err = os.WriteFile(filepath.Join(root, "europepmc-fulltext.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("doi=%s bytes=%d sections=%d anchors=%d revision=%s", record.Source.NativeID, len(record.Text),
		len(record.Source.Sections), len(record.Source.Anchors), record.Source.Revision)
}
