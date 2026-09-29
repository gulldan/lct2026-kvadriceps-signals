package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

type textOp struct {
	size int
	text string
}

// testPDF собирает многостраничный PDF со стандартным шрифтом Helvetica и
// корректной таблицей xref: минимум, который pdfium читает без подстановок.
func testPDF(pages ...[]textOp) []byte {
	objs := make([]string, 0, 3+2*len(pages))
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	kids := make([]string, 0, len(pages))
	for _, ops := range pages {
		var stream strings.Builder
		for i, op := range ops {
			fmt.Fprintf(&stream, "BT /F1 %d Tf 72 %d Td (%s) Tj ET\n", op.size, 700-40*i, op.text)
		}
		pageObj := len(objs) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObj))
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", pageObj+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", stream.Len(), stream.String()))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func newExtractor(t *testing.T) *Extractor {
	t.Helper()
	e, err := New(t.Context(), 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func TestExtractWithLayout(t *testing.T) {
	e := newExtractor(t)
	body := []textOp{{10, "Body text of the section goes here."}, {10, "Another body line with more words."}}
	pdf := testPDF(
		append([]textOp{{16, "1. Introduction"}}, body...),
		append([]textOp{{16, "2. Methods"}}, body...),
		append([]textOp{{10, "III. RESULTS AND DISCUSSION"}, {10, "T + R"}}, body...),
	)
	doc, err := e.ExtractWithLayout(t.Context(), pdf)
	if err != nil {
		t.Fatalf("ExtractWithLayout: %v", err)
	}
	runes := []rune(doc.Text)
	if len(doc.PageOffsets) != 3 || doc.PageOffsets[0] != 0 {
		t.Fatalf("PageOffsets = %v, want three pages from 0", doc.PageOffsets)
	}
	if got := string(runes[doc.PageOffsets[1]:]); !strings.HasPrefix(got, "2. Methods") {
		t.Fatalf("page 2 starts with %q", got[:min(20, len(got))])
	}
	if len(doc.SectionOffsets) != 3 {
		t.Fatalf("SectionOffsets = %+v, want two numbered headings and the same-size caps one", doc.SectionOffsets)
	}
	for i, want := range []string{"1. Introduction", "2. Methods", "III. RESULTS AND DISCUSSION"} {
		s := doc.SectionOffsets[i]
		if s.Heading != want || !strings.HasPrefix(string(runes[s.Rune:]), want) {
			t.Errorf("section %d = %+v, text there %q", i, s, string(runes[s.Rune:min(len(runes), s.Rune+20)]))
		}
	}
	if !strings.Contains(doc.Text, "Another body line") {
		t.Fatalf("body text lost: %q", doc.Text)
	}
}

func TestExtractEdgeCases(t *testing.T) {
	e := newExtractor(t)
	text, err := e.Extract(t.Context(), testPDF([]textOp{}))
	if err != nil || strings.TrimSpace(text) != "" {
		t.Fatalf("empty page: text=%q err=%v", text, err)
	}
	if _, err := e.Extract(t.Context(), []byte("not a pdf")); err == nil {
		t.Fatal("garbage bytes must fail to open")
	}
}

func TestPDFDate(t *testing.T) {
	cases := map[string]string{
		"D:20260902123456+03'00'": "2026-09-02T09:34:56Z",
		"D:20260902123456Z":       "2026-09-02T12:34:56Z",
		"D:20260902":              "2026-09-02T00:00:00Z",
		"garbage":                 "",
		"":                        "",
	}
	for in, want := range cases {
		if got := pdfDate(in); got != want {
			t.Errorf("pdfDate(%q) = %q, want %q", in, got, want)
		}
	}
}
