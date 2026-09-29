// Package pdf извлекает текстовый слой PDF движком pdfium, собранным в
// WebAssembly: без CGO и внешних процессов, поэтому образ остаётся distroless.
package pdf

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	"github.com/example/pdf-parser/internal/domain"
)

// capsWordRe — слово капительного заголовка: буквы с пунктуацией внутри слова
// либо номер раздела («IV.», «2.1»); знаки операций и скобки не проходят.
var capsWordRe = regexp.MustCompile(`^(\p{L}[\p{L}.,:;'’\-–]*|\d+(\.\d+)*\.?)$`)

const (
	// Заголовок крупнее текста (arXiv: 12 pt против 11), но не в разы — подписи к рисункам и титул крупнее
	// в три-пять раз. При равном кегле (IEEE) текстовая эвристика ловит обрывки формул, там — только капитель.
	headingMinScale = 1.05
	headingMaxScale = 2.5
	bodyScale       = 0.95
	capsMaxWords    = 6

	// wasmMemoryPages ограничивает память экземпляра pdfium 256 МиБ (страницы по 64 КиБ). Память только растёт,
	// и без предела одна страница съедала 9,5 ГиБ и роняла контейнер по OOM; с пределом это ошибка одного документа.
	wasmMemoryPages = 4096
)

type Extractor struct{ pool pdfium.Pool }

// New компилирует модуль pdfium один раз; экземпляр живёт один документ, чтобы память не копилась между файлами.
func New(ctx context.Context, workers int) (*Extractor, error) {
	pool, err := webassembly.Init(webassembly.Config{
		Context: ctx, MaxIdle: workers, MaxTotal: workers,
		// Файлы приходят байтами из S3; доступ модуля к файловой системе не нужен.
		FSConfig: wazero.NewFSConfig(),
		// Набор возможностей обязан повторять умолчание библиотеки: в модуле
		// pdfium setjmp/longjmp собраны в инструкции обработки исключений, и без
		// этой возможности модуль просто не компилируется.
		RuntimeConfig: wazero.NewRuntimeConfig().
			WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
			WithMemoryLimitPages(wasmMemoryPages),
	})
	if err != nil {
		return nil, fmt.Errorf("init pdfium: %w", err)
	}
	return &Extractor{pool: pool}, nil
}

func (e *Extractor) Close() error {
	if err := e.pool.Close(); err != nil {
		return fmt.Errorf("close pdfium: %w", err)
	}
	return nil
}

func (e *Extractor) Extract(ctx context.Context, data []byte) (string, error) {
	doc, err := e.ExtractWithLayout(ctx, data)
	return doc.Text, err
}

type line struct {
	byteStart, byteEnd, runeStart int
	size                          float64
}

// assembler считает границы строк и страниц в рунах — так требует контракт chunk-splitter.
type assembler struct {
	b       strings.Builder
	runes   int
	lines   []line
	cur     line
	offsets []int
	sizes   map[int]int
}

func (a *assembler) startPage() {
	if len(a.offsets) > 0 {
		a.b.WriteByte('\n')
		a.runes++
	}
	a.offsets = append(a.offsets, a.runes)
	a.cur = line{byteStart: a.b.Len(), runeStart: a.runes}
}

func (a *assembler) add(ch *responses.GetPageTextStructuredChar) {
	switch ch.Text {
	case "\r":
		return
	case "\n":
		a.endLine()
		a.b.WriteByte('\n')
		a.runes++
		a.cur = line{byteStart: a.b.Len(), runeStart: a.runes}
		return
	}
	a.b.WriteString(ch.Text)
	a.runes += utf8.RuneCountInString(ch.Text)
	if size := fontSize(ch.FontInformation); size > 0 && !isSpace(ch.Text) {
		a.cur.size = max(a.cur.size, size)
		a.sizes[int(math.Round(size))]++
	}
}

func (a *assembler) endLine() {
	a.cur.byteEnd = a.b.Len()
	a.lines = append(a.lines, a.cur)
}

func (a *assembler) sections(text string) []domain.SectionOffset {
	body := float64(bodyFontSize(a.sizes))
	var out []domain.SectionOffset
	for _, l := range a.lines {
		if body == 0 || l.size < body*bodyScale || l.size > body*headingMaxScale {
			continue
		}
		raw := text[l.byteStart:l.byteEnd]
		if detectHeading(raw) && realWord(raw) && (l.size >= body*headingMinScale || capsHeading(raw)) {
			out = append(out, domain.SectionOffset{Rune: l.runeStart + leadingSpaceRunes(raw), Heading: collapse(raw)})
		}
	}
	return out
}

type session struct {
	inst pdfium.Pdfium
	doc  references.FPDF_DOCUMENT
}

func (e *Extractor) open(ctx context.Context, data []byte) (session, error) {
	inst, err := e.pool.GetInstanceWithContext(ctx)
	if err != nil {
		return session{}, fmt.Errorf("pdfium instance: %w", err)
	}
	opened, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		_ = inst.Close()
		return session{}, fmt.Errorf("open pdf: %w", err)
	}
	return session{inst: inst, doc: opened.Document}, nil
}

func (s session) close() {
	_, _ = s.inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: s.doc})
	_ = s.inst.Close()
}

func (s session) pageChars(i int) ([]*responses.GetPageTextStructuredChar, error) {
	page, err := s.inst.GetPageTextStructured(&requests.GetPageTextStructured{
		Page:                   requests.Page{ByIndex: &requests.PageByIndex{Document: s.doc, Index: i}},
		Mode:                   requests.GetPageTextStructuredModeChars,
		CollectFontInformation: true,
	})
	if err != nil {
		return nil, fmt.Errorf("page text: %w", err)
	}
	return page.Chars, nil
}

// ExtractWithLayout идёт посимвольно: кегль каждой строки нужен для поиска заголовков.
func (e *Extractor) ExtractWithLayout(ctx context.Context, data []byte) (domain.ExtractedDocument, error) {
	s, err := e.open(ctx, data)
	if err != nil {
		return domain.ExtractedDocument{}, err
	}
	defer func() { s.close() }()

	count, err := s.inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: s.doc})
	if err != nil {
		return domain.ExtractedDocument{}, fmt.Errorf("page count: %w", err)
	}
	a := assembler{sizes: map[int]int{}}
	skipped := 0
	for i := range count.PageCount {
		a.startPage()
		chars, perr := s.pageChars(i)
		if perr != nil {
			if !strings.Contains(perr.Error(), "wasm error") {
				return domain.ExtractedDocument{}, fmt.Errorf("page %d text: %w", i+1, perr)
			}
			// При нехватке линейной памяти pdfium падает на unreachable, и экземпляр мёртв. Страница остаётся пустой,
			// экземпляр пересоздаётся: остальной текст ценнее отказа из-за одной страницы.
			s.close()
			if s, err = e.open(ctx, data); err != nil {
				return domain.ExtractedDocument{}, err
			}
			skipped++
			continue
		}
		for _, ch := range chars {
			a.add(ch)
		}
		a.endLine()
	}
	if skipped > 0 && skipped == count.PageCount {
		return domain.ExtractedDocument{}, fmt.Errorf(
			"every page stopped the pdfium engine; the %d MiB memory limit is the usual cause", wasmMemoryPages/16)
	}
	text := a.b.String()
	return domain.ExtractedDocument{
		Text:           text,
		PageOffsets:    a.offsets,
		SectionOffsets: a.sections(text),
		SkippedPages:   skipped,
		Author:         metaText(s.inst, s.doc, "Author"),
		PublishedAt:    pdfDate(metaText(s.inst, s.doc, "CreationDate")),
	}, nil
}

// realWord отсекает крупно набранные обозначения формул («X», «X𝒳»): в заголовке
// должно быть слово хотя бы из двух букв обычного письма, а не математического
// алфавита.
func realWord(raw string) bool {
	for _, w := range strings.Fields(raw) {
		letters := 0
		for _, r := range w {
			if unicode.In(r, unicode.Latin, unicode.Cyrillic, unicode.Han) {
				letters++
			}
		}
		if letters >= 2 {
			return true
		}
	}
	return false
}

// capsHeading ловит разделы IEEE, набранные капителью тем же кеглем, что и текст. Обрывки формул («T + R»)
// отсекаются: каждое слово — буквенное или номер раздела, и хотя бы одно не короче четырёх букв.
func capsHeading(raw string) bool {
	words := strings.Fields(raw)
	if len(words) == 0 || len(words) > capsMaxWords || !mostlyUppercase(raw) {
		return false
	}
	long := false
	for _, w := range words {
		if !capsWordRe.MatchString(w) {
			return false
		}
		letters := 0
		for _, r := range w {
			if !unicode.IsLetter(r) {
				continue
			}
			// Математические алфавиты («𝒳») — тоже буквы по Unicode, но это формула.
			if !unicode.In(r, unicode.Latin, unicode.Cyrillic) {
				return false
			}
			letters++
		}
		long = long || letters >= 4
	}
	return long
}

// fontSize берёт кегль с учётом матрицы текста: в некоторых PDF шрифт объявлен
// размером 1 и масштабируется матрицей, тогда номинальный Size бесполезен.
func fontSize(fi *responses.FontInformation) float64 {
	if fi == nil {
		return 0
	}
	if fi.RenderedSize > 0 {
		return fi.RenderedSize
	}
	return fi.Size
}

func isSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func bodyFontSize(sizes map[int]int) int {
	body, most := 0, 0
	for size, n := range sizes {
		if n > most || (n == most && size < body) {
			body, most = size, n
		}
	}
	return body
}

func metaText(inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, tag string) string {
	resp, err := inst.FPDF_GetMetaText(&requests.FPDF_GetMetaText{Document: doc, Tag: tag})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(resp.Value)
}

// pdfDate переводит дату из словаря документа ("D:20260902123456+03'00'") в
// RFC 3339; нераспознанное значение опускается.
func pdfDate(s string) string {
	s = strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(s), "D:"), "'", "")
	for _, layout := range []string{
		"20060102150405-0700", "20060102150405Z0000", "20060102150405Z", "20060102150405",
		"200601021504", "2006010215", "20060102", "200601", "2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}
