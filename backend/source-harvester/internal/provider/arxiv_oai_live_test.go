package provider

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Окно — объявленные закрытые два дня обновлений; отбор всё равно по дате v1, пересмотренные старые работы не берутся.
func TestLiveArxivOAI(t *testing.T) {
	if os.Getenv("TR_ARXIV_OAI_LIVE") != "1" {
		t.Skip("set TR_ARXIV_OAI_LIVE=1 for the bounded official arXiv OAI/PDF check")
	}
	scope := Scope{Query: "retrieval", From: "2026-09-10", Through: "2026-09-11", Category: "cs.CL", PageSize: 100}
	a := NewArXiv()
	a.base = "https://oaipmh.arxiv.org/oai"
	var pages []Page
	var selected Record
	var progress PageProgress
	cursor, scanned, matched := "", 0, 0
	for range 4 {
		page, err := a.Fetch(t.Context(), scope, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = progress.Observe(&page); err != nil {
			t.Fatal(err)
		}
		pages = append(pages, page)
		scanned, matched = scanned+page.Received, matched+len(page.Records)
		for _, record := range page.Records {
			if record.Source.Publication.Value < scope.From || record.Source.Publication.Value > scope.Through ||
				record.Source.RevisionPublication.Value > scope.Through || len(record.Text) < 100 || record.Source.ArXiv.Version < 1 {
				t.Fatal("live OAI record lost publication/version bounds or its abstract")
			}
			if selected.Source.NativeID == "" {
				selected = record
			}
		}
		if page.Exhausted {
			break
		}
		cursor = page.Next
	}
	if matched == 0 {
		t.Fatalf("bounded OAI scan returned no current topic matches after %d records", scanned)
	}
	pdf, err := a.PDF(t.Context(), selected.Source)
	if err != nil || len(pdf) < 10000 {
		t.Fatalf("versioned arXiv PDF was not accessible: bytes=%d err=%v", len(pdf), err)
	}
	// Идентичность источника не зависит от responseDate OAI и перезапуска: по ней дедупликация в базе.
	// Общего шлюза в базе у двух экземпляров нет, поэтому паузу arXiv выдерживает тест.
	if err = waitContext(t.Context(), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	replay := NewArXiv()
	replay.base = a.base
	last := pages[len(pages)-1]
	repeated, err := replay.Fetch(t.Context(), scope, last.Cursor)
	if err != nil || len(repeated.Records) != len(last.Records) {
		t.Fatalf("live persisted OAI cursor cannot resume after restart: %v", err)
	}
	for i, record := range repeated.Records {
		if record.Source.NativeID != last.Records[i].Source.NativeID ||
			record.Source.Revision != last.Records[i].Source.Revision || record.Source.ContentHash != last.Records[i].Source.ContentHash {
			t.Fatal("repeat observation changed immutable document identity")
		}
	}
	if dir := os.Getenv("TR_SOURCE_PROOF_DIR"); dir != "" {
		if err = os.MkdirAll(dir, 0750); err != nil {
			t.Fatal(err)
		}
		raw, marshalErr := json.Marshal(struct {
			Pages       []Page `json:"pages"`
			Scanned     int    `json:"scanned"`
			Matched     int    `json:"matched"`
			PDFRevision string `json:"pdf_revision"`
			PDFBytes    int    `json:"pdf_bytes"`
			PDFHash     string `json:"pdf_sha256"`
		}{pages, scanned, matched, selected.Source.Revision, len(pdf), digest(pdf)})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err = os.WriteFile(filepath.Join(dir, "arxiv-oai.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("scanned=%d matched=%d pages=%d pdf=%s bytes=%d sha256=%s replay_identity=stable", scanned, matched,
		len(pages), selected.Source.Revision, len(pdf), digest(pdf))
}
