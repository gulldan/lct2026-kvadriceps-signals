package office

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/office-parser/internal/domain"
	"github.com/example/office-parser/internal/platform/logger"
	"github.com/xuri/excelize/v2"
)

type Extractor struct {
	tikaURL     string
	workbookURL string
	http        *http.Client
}

func NewExtractor(tikaURL string) *Extractor {
	return NewExtractorWithWorkbookParser(tikaURL, "")
}

func NewExtractorWithWorkbookParser(tikaURL, workbookParserURL string) *Extractor {
	return &Extractor{
		tikaURL:     strings.TrimRight(tikaURL, "/"),
		workbookURL: strings.TrimRight(workbookParserURL, "/"),
		http:        &http.Client{Timeout: 60 * time.Second},
	}
}

// Extract ловит панику: библиотеки разбора падают на битых файлах из реального мира.
func (e *Extractor) Extract(
	ctx context.Context, data []byte, filename, mimeType string,
) (result domain.ExtractionResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("office parsing panicked: %v", r)
		}
	}()

	var text string
	switch kind := classify(filename, mimeType); kind {
	case kindDocx:
		text, err = extractDocx(data)
	case kindPptx:
		text, err = extractPptx(data)
	case kindXlsx:
		if e.workbookURL != "" {
			wb, werr := e.workbook(ctx, data, filename)
			if werr == nil && strings.TrimSpace(wb.Text) != "" {
				return wb, nil
			}
			if werr != nil {
				logger.From(ctx).Warn().Err(werr).Str("filename", filename).Msg("workbook parser failed, falling back to excelize text")
			}
		}
		text, err = extractXlsx(data)
		result.Metadata = flatWorkbookMetadata("xlsx", "excelize")
	case kindLegacyXls:
		result, err = e.tika(ctx, data, filename)
		if err != nil {
			return domain.ExtractionResult{}, err
		}
		if strings.TrimSpace(result.Text) != "" {
			result.Metadata = mergeMetadata(result.Metadata, degradedWorkbookMetadata("xls", "tika"))
		}
		return result, nil
	case kindText:
		// Tika падает на сотнях мегабайт текста; обычно процессор передаёт текст ссылкой, этот путь — для прямых вызовов.
		return domain.ExtractionResult{Text: string(bytes.ToValidUTF8(data, []byte("�")))}, nil
	case kindUnknown:
		return e.tika(ctx, data, filename)
	}
	if err != nil {
		return domain.ExtractionResult{}, err
	}
	result.Text = text

	// Документ из одних картинок нативно пуст, но Tika может достать из него текст.
	if strings.TrimSpace(result.Text) == "" && e.tikaURL != "" {
		logger.From(ctx).Info().Str("filename", filename).Msg("no text extracted, falling back to Tika")
		return e.tika(ctx, data, filename)
	}
	return result, nil
}

func (e *Extractor) IsPlainText(filename, mimeType string) bool {
	return classify(filename, mimeType) == kindText
}

type docKind int

const (
	kindUnknown docKind = iota
	kindDocx
	kindPptx
	kindXlsx
	kindLegacyXls
	kindText
)

func classify(filename, mimeType string) docKind {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return kindDocx
	case ".pptx":
		return kindPptx
	// Макро-книги и шаблоны — тот же OOXML-контейнер, excelize читает их так же.
	case ".xlsx", ".xlsm", ".xltx", ".xltm":
		return kindXlsx
	case ".xls":
		return kindLegacyXls
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".log":
		return kindText
	}
	mimeType = strings.ToLower(mimeType)
	switch {
	case strings.Contains(mimeType, "wordprocessingml"):
		return kindDocx
	case strings.Contains(mimeType, "presentationml"):
		return kindPptx
	case strings.Contains(mimeType, "spreadsheetml"),
		strings.Contains(mimeType, "ms-excel.sheet"):
		return kindXlsx
	case mimeType == "application/vnd.ms-excel":
		return kindLegacyXls
	case strings.HasPrefix(mimeType, "text/"):
		return kindText
	}
	return kindUnknown
}

func extractDocx(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open docx zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return "", fmt.Errorf("open word/document.xml: %w", oerr)
		}
		text, terr := collectText(rc, "t")
		_ = rc.Close()
		return text, terr
	}
	return "", nil
}

// extractPptx обходит слайды в порядке архива, а не по номерам.
func extractPptx(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open pptx zip: %w", err)
	}
	var b strings.Builder
	for _, f := range zr.File {
		name := f.Name
		if !strings.HasPrefix(name, "ppt/slides/slide") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open %s: %w", name, err)
		}
		slide, err := collectText(rc, "t")
		_ = rc.Close()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		if slide == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(slide)
	}
	return b.String(), nil
}

// Между фрагментами ставится пробел, иначе соседние слова слипаются.
func collectText(r io.Reader, local string) (string, error) {
	dec := xml.NewDecoder(r)
	var b strings.Builder
	inRun := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == local {
				inRun = true
			}
		case xml.EndElement:
			if t.Name.Local == local {
				inRun = false
			}
		case xml.CharData:
			if inRun {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.Write(t)
			}
		}
	}
	return b.String(), nil
}

func extractXlsx(data []byte) (string, error) {
	fx, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = fx.Close() }()

	type sheetText struct{ name, text string }
	var sheets []sheetText
	for _, sheet := range fx.GetSheetList() {
		rows, err := fx.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		var b strings.Builder
		for _, row := range rows {
			line := rowLine(row)
			if line == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
		}
		if b.Len() > 0 {
			sheets = append(sheets, sheetText{name: sheet, text: b.String()})
		}
	}

	var b strings.Builder
	for i, s := range sheets {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if len(sheets) > 1 {
			b.WriteString("Лист: " + s.name + "\n")
		}
		b.WriteString(s.text)
	}
	return b.String(), nil
}

func rowLine(row []string) string {
	last := -1
	for i, c := range row {
		if strings.TrimSpace(c) != "" {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	return strings.Join(row[:last+1], " | ")
}

func (e *Extractor) tika(ctx context.Context, data []byte, filename string) (domain.ExtractionResult, error) {
	if e.tikaURL == "" {
		return domain.ExtractionResult{}, nil
	}
	url := e.tikaURL + "/tika/text"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return domain.ExtractionResult{}, fmt.Errorf("build tika request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := e.http.Do(req)
	if err != nil {
		return domain.ExtractionResult{}, fmt.Errorf("call tika: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ExtractionResult{}, fmt.Errorf("read tika response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return domain.ExtractionResult{}, fmt.Errorf(
			"tika %s: status %d: %s", filename, resp.StatusCode, strings.TrimSpace(string(body)),
		)
	}
	return domain.ExtractionResult{Text: string(body)}, nil
}
