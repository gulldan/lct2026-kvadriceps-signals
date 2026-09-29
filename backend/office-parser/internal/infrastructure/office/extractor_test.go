package office_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/example/office-parser/internal/domain"
	"github.com/example/office-parser/internal/infrastructure/office"
)

var _ domain.TextExtractor = (*office.Extractor)(nil)

func buildXLSX(t *testing.T, rows [][]string) []byte {
	t.Helper()
	fx := excelize.NewFile()
	defer func() { _ = fx.Close() }()
	for r, row := range rows {
		for c, val := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("coordinates: %v", err)
			}
			if err := fx.SetCellValue("Sheet1", cell, val); err != nil {
				t.Fatalf("set cell %s: %v", cell, err)
			}
		}
	}
	if _, err := fx.NewSheet("Empty"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	buf, err := fx.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

func mustExtract(t *testing.T, e *office.Extractor, data []byte, filename, mimeType string) domain.ExtractionResult {
	t.Helper()
	result, err := e.Extract(context.Background(), data, filename, mimeType)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return result
}

type zipPart struct{ name, content string }

func buildZip(t *testing.T, parts ...zipPart) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatalf("zip create %s: %v", p.name, err)
		}
		if _, err := io.WriteString(w, p.content); err != nil {
			t.Fatalf("zip write %s: %v", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func docxXML(runs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><w:document xmlns:w="ns"><w:body><w:p>`)
	for _, r := range runs {
		b.WriteString("<w:r><w:t>" + r + "</w:t></w:r>")
	}
	b.WriteString(`</w:p></w:body></w:document>`)
	return b.String()
}

func pptxXML(runs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><p:sld xmlns:a="ns" xmlns:p="ns"><p:cSld><p:spTree>`)
	for _, r := range runs {
		b.WriteString("<a:p><a:r><a:t>" + r + "</a:t></a:r></a:p>")
	}
	b.WriteString(`</p:spTree></p:cSld></p:sld>`)
	return b.String()
}

func TestIsPlainText(t *testing.T) {
	e := office.NewExtractor("")
	cases := []struct {
		name, file, mime string
		want             bool
	}{
		{"txt extension", "notes.txt", "", true},
		{"md extension", "README.md", "", true},
		{"csv extension", "data.csv", "application/octet-stream", true},
		{"text mime", "blob", "text/x-go", true},
		{"docx not plain", "a.docx", "", false},
		{"unknown not plain", "a.bin", "application/octet-stream", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.IsPlainText(tc.file, tc.mime); got != tc.want {
				t.Fatalf("IsPlainText(%q, %q) = %v, want %v", tc.file, tc.mime, got, tc.want)
			}
		})
	}
}

func TestExtractXLSX(t *testing.T) {
	data := buildXLSX(t, [][]string{{"Name", "Age"}, {"Alice", "30"}})
	result := mustExtract(t, office.NewExtractor(""), data, "people.xlsx", "")
	want := "Name | Age\nAlice | 30"
	if result.Text != want {
		t.Fatalf("Extract xlsx = %q, want %q", result.Text, want)
	}
	if result.Metadata["workbook_mode"] != "flat_text" {
		t.Fatalf("workbook_mode = %q, want flat_text", result.Metadata["workbook_mode"])
	}
}

func TestExtractXLSXViaWorkbookParser(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		_ = file.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"| source_uri | A |\n| --- | --- |\n| book.xlsx#sheet=Sheet1&range=A1:A1 | Ni |",`+
			`"metadata":{"workbook_mode":"anchored_markdown","workbook_parser_engine":"calamine"},`+
			`"sidecars":[{"name":"workbook.sidecar.json","content_type":"application/json","text":"{\"cells\":[]}"}]}`)
	}))
	defer srv.Close()

	result := mustExtract(t, office.NewExtractorWithWorkbookParser("", srv.URL), []byte("xlsx bytes"), "book.xlsx", "")
	if result.Text == "" || !strings.Contains(result.Text, "source_uri") {
		t.Fatalf("structured workbook text missing anchors: %q", result.Text)
	}
	if result.Metadata["workbook_mode"] != "anchored_markdown" {
		t.Fatalf("workbook_mode = %q, want anchored_markdown", result.Metadata["workbook_mode"])
	}
	if len(result.Sidecars) != 1 || result.Sidecars[0].Name != "workbook.sidecar.json" {
		t.Fatalf("sidecars = %+v, want workbook.sidecar.json", result.Sidecars)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/parse" {
		t.Fatalf("workbook parser request = %s %s, want POST /v1/parse", gotMethod, gotPath)
	}
}

func TestExtractXLSXWorkbookParserFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	data := buildXLSX(t, [][]string{{"Name", "Age"}, {"Alice", "30"}})
	result := mustExtract(t, office.NewExtractorWithWorkbookParser("", srv.URL), data, "people.xlsx", "")
	if result.Text != "Name | Age\nAlice | 30" {
		t.Fatalf("fallback xlsx text = %q", result.Text)
	}
	if result.Metadata["workbook_mode"] != "flat_text" || result.Metadata["table_fidelity"] != "low" {
		t.Fatalf("fallback metadata = %+v, want flat_text/low", result.Metadata)
	}
}

func TestExtractXLSX_MultiSheet(t *testing.T) {
	fx := excelize.NewFile()
	defer func() { _ = fx.Close() }()
	if err := fx.SetCellValue("Sheet1", "A1", "Метрика"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := fx.SetCellValue("Sheet1", "B1", "Значение"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if _, err := fx.NewSheet("Данные"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	// A3 оставляет перед собой пустую строку, C-хвост в строке остаётся пустым.
	if err := fx.SetCellValue("Данные", "A3", "42"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	buf, err := fx.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	got := mustExtract(t, office.NewExtractor(""), buf.Bytes(), "book.xlsm", "").Text
	want := "Лист: Sheet1\nМетрика | Значение\n\nЛист: Данные\n42"
	if got != want {
		t.Fatalf("Extract multi-sheet = %q, want %q", got, want)
	}
}

func TestExtractDocx(t *testing.T) {
	data := buildZip(t,
		zipPart{"word/document.xml", docxXML("Hello", "World")},
		zipPart{"docProps/app.xml", "<ignored/>"},
	)
	got := mustExtract(t, office.NewExtractor(""), data, "doc.docx", "").Text
	if got != "Hello World" {
		t.Fatalf("Extract docx = %q, want %q", got, "Hello World")
	}
}

func TestExtractDocxByMIME(t *testing.T) {
	data := buildZip(t, zipPart{"word/document.xml", docxXML("Mime", "Routed")})
	mime := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	got := mustExtract(t, office.NewExtractor(""), data, "blob", mime).Text
	if got != "Mime Routed" {
		t.Fatalf("Extract docx by mime = %q, want %q", got, "Mime Routed")
	}
}

func TestExtractDocxNoDocumentPart(t *testing.T) {
	data := buildZip(t, zipPart{"docProps/core.xml", "<x/>"})
	got := mustExtract(t, office.NewExtractor(""), data, "doc.docx", "").Text
	if got != "" {
		t.Fatalf("Extract docx without part = %q, want empty", got)
	}
}

func TestExtractPptx(t *testing.T) {
	data := buildZip(t,
		zipPart{"ppt/slides/slide1.xml", pptxXML("First", "slide")},
		zipPart{"ppt/slides/slide2.xml", pptxXML("Second")},
		zipPart{"ppt/slides/slide3.xml", pptxXML()},
		zipPart{"ppt/notesSlides/n.xml", pptxXML("ignored")},
	)
	got := mustExtract(t, office.NewExtractor(""), data, "deck.pptx", "").Text
	want := "First slide\n\nSecond"
	if got != want {
		t.Fatalf("Extract pptx = %q, want %q", got, want)
	}
}

func TestExtractPlainText(t *testing.T) {
	data := append([]byte("ok"), 0xff)
	got := mustExtract(t, office.NewExtractor(""), data, "log.txt", "").Text
	if got != "ok�" {
		t.Fatalf("Extract text = %q, want %q", got, "ok�")
	}
}

func TestExtractMalformedZip(t *testing.T) {
	for _, file := range []string{"broken.docx", "broken.pptx"} {
		t.Run(file, func(t *testing.T) {
			_, err := office.NewExtractor("").Extract(context.Background(), []byte("not a zip"), file, "")
			if err == nil {
				t.Fatalf("Extract(%s) expected error, got nil", file)
			}
		})
	}
}

func TestExtractMalformedXLSX(t *testing.T) {
	_, err := office.NewExtractor("").Extract(context.Background(), []byte("garbage"), "book.xlsx", "")
	if err == nil {
		t.Fatalf("Extract xlsx expected error, got nil")
	}
}

func TestExtractDocxBadXML(t *testing.T) {
	data := buildZip(t, zipPart{"word/document.xml", "<w:t>unterminated"})
	_, err := office.NewExtractor("").Extract(context.Background(), data, "doc.docx", "")
	if err == nil {
		t.Fatalf("Extract bad xml expected error, got nil")
	}
}

func TestExtractUnknownNoTika(t *testing.T) {
	got := mustExtract(t, office.NewExtractor(""), []byte("data"), "file.bin", "application/octet-stream")
	if got.Text != "" {
		t.Fatalf("Extract unknown = %q, want empty", got.Text)
	}
}

func TestExtractUnknownViaTika(t *testing.T) {
	var gotMethod, gotPath, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAccept = r.Method, r.URL.Path, r.Header.Get("Accept")
		_, _ = io.WriteString(w, "tika text")
	}))
	defer srv.Close()

	got := mustExtract(t, office.NewExtractor(srv.URL+"/"), []byte("x"), "file.bin", "application/octet-stream")
	if got.Text != "tika text" {
		t.Fatalf("Extract via tika = %q, want %q", got.Text, "tika text")
	}
	if gotMethod != http.MethodPut || gotPath != "/tika/text" || gotAccept != "text/plain" {
		t.Fatalf("tika request = %s %s accept=%q, want PUT /tika/text accept=text/plain", gotMethod, gotPath, gotAccept)
	}
}

func TestExtractEmptyKnownFallsBackToTika(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "from tika")
	}))
	defer srv.Close()

	data := buildXLSX(t, nil)
	got := mustExtract(t, office.NewExtractor(srv.URL), data, "empty.xlsx", "")
	if got.Text != "from tika" {
		t.Fatalf("Extract empty xlsx = %q, want %q", got.Text, "from tika")
	}
}

func TestExtractLegacyXLSViaTika(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "legacy table text")
	}))
	defer srv.Close()

	got := mustExtract(t, office.NewExtractor(srv.URL), []byte("xls bytes"), "old.xls", "")
	if got.Text != "legacy table text" {
		t.Fatalf("Extract legacy xls = %q, want tika text", got.Text)
	}
	if got.Metadata["workbook_mode"] != "degraded_text" || got.Metadata["workbook_format"] != "xls" {
		t.Fatalf("legacy metadata = %+v, want degraded xls", got.Metadata)
	}
}

func TestExtractTikaErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	_, err := office.NewExtractor(srv.URL).Extract(context.Background(), []byte("x"), "file.bin", "application/octet-stream")
	if err == nil {
		t.Fatalf("Extract expected tika status error, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("Extract error = %v, want status 500", err)
	}
}

func TestExtractTikaTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := office.NewExtractor(url).Extract(context.Background(), []byte("x"), "file.bin", "application/octet-stream")
	if err == nil {
		t.Fatalf("Extract expected transport error, got nil")
	}
}
