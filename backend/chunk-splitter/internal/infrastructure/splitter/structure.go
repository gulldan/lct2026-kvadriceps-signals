package splitter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/sourcedoc"
)

// Обычные разделители резали бы таблицу или список условий посреди строки, поэтому табличные блоки
// держатся целыми, а подписи — со своим блоком. Детекция предпочитает промах ложному срабатыванию.

const (
	// Двух табличных строк достаточно для узора; одинокая перенесённая фраза с разрывом всё равно отсекается.
	minTableRows = 2
	// Подписи коротки: длинный абзац, начинающийся с «Рисунок», — это текст.
	maxCaptionRunes = 200
	maxHeadingRunes = 64
	// Целая таблица может превысить бюджет не больше чем во столько раз, иначе «таблица» размером
	// с документ отменит окна.
	tableOverageFactor = 3
)

const (
	secAbstract     = "Abstract"
	secIntroduction = "Introduction"
	secMethods      = "Methods"
	secExperiments  = "Experiments"
	secResults      = "Results"
	secDiscussion   = "Discussion"
	secLimitations  = "Limitations"
	secConclusion   = "Conclusion"
	secReferences   = "References"
	secAcknowledge  = "Acknowledgements"
	secBackground   = "Background"
	secSummary      = "Summary"
	secDescription  = "Description"
	secClaims       = "Claims"
)

type SectionMark struct {
	Rune    int
	Heading string
}

// SectionMarks повторно использует поиск заголовков предпрохода, чтобы раздел чанка и карта разделов
// документа не расходились.
func SectionMarks(text string) []SectionMark {
	marks := []SectionMark{}
	at := 0
	for _, l := range splitKeepNewlines(text) {
		if h := sectionHeading(l.content); h != "" {
			marks = append(marks, SectionMark{Rune: at, Heading: h})
		}
		at += utf8.RuneCountInString(l.raw)
	}
	return marks
}

type blockKind int

const (
	kindFlow blockKind = iota
	kindTable
)

type block struct {
	text    string
	kind    blockKind
	section string
}

func segment(text string) []block {
	lines := splitKeepNewlines(text)

	var (
		blocks  []block
		flow    strings.Builder
		section string
	)
	flushFlow := func() {
		if flow.Len() == 0 {
			return
		}
		blocks = append(blocks, block{text: flow.String(), kind: kindFlow, section: section})
		flow.Reset()
	}

	for i := 0; i < len(lines); {
		// Одинокая подпись прямо над таблицей присоединяется к ней, чтобы оказаться в одном чанке.
		if from, run := captionThenTable(lines, i); run > 0 {
			flushFlow()
			b, next := captionedTable(lines, from, i+run, section)
			blocks = append(blocks, b)
			i = next
			continue
		}
		if run := tableRun(lines, i); run > 0 {
			flushFlow()
			b, next := captionedTable(lines, i, i+run, section)
			blocks = append(blocks, b)
			i = next
			continue
		}
		if h := sectionHeading(lines[i].content); h != "" {
			// Заголовок и перепомечает следующий текст, и остаётся частью потока.
			section = h
			flow.WriteString(lines[i].raw)
			i++
			continue
		}
		flow.WriteString(lines[i].raw)
		i++
	}
	flushFlow()

	return attachCaptions(blocks)
}

func captionThenTable(lines []line, i int) (from, run int) {
	if i+1 >= len(lines) || !isCaptionLine(lines[i].content) {
		return i, 0
	}
	if t := tableRun(lines, i+1); t > 0 {
		return i, 1 + t
	}
	return i, 0
}

func captionedTable(lines []line, from, to int, section string) (block, int) {
	if to < len(lines) && isCaptionLine(lines[to].content) {
		to++
	}
	return block{text: joinRaw(lines[from:to]), kind: kindTable, section: section}, to
}

// raw хранится с переводом строки, чтобы блоки восстанавливали исходник байт в байт.
type line struct {
	content string
	raw     string
}

func splitKeepNewlines(s string) []line {
	var out []line
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, line{content: s[start:i], raw: s[start : i+1]})
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, line{content: s[start:], raw: s[start:]})
	}
	return out
}

func tableRun(lines []line, i int) int {
	n := 0
	for i+n < len(lines) && isTabularLine(lines[i+n].content) {
		n++
	}
	if n >= minTableRows {
		return n
	}
	return 0
}

func isTabularLine(content string) bool {
	s := strings.TrimRight(content, " \t")
	if strings.TrimSpace(s) == "" {
		return false
	}
	if strings.Contains(s, "\t") {
		return hasTwoCells(s, "\t")
	}
	if strings.Contains(s, "|") {
		return hasTwoCells(s, "|")
	}
	return hasMultiSpaceColumns(s)
}

// Две непустые ячейки, чтобы ведущий или замыкающий «|» не приняли за границу колонки.
func hasTwoCells(s, sep string) bool {
	nonEmpty := 0
	for _, c := range strings.Split(s, sep) {
		if strings.TrimSpace(c) != "" {
			nonEmpty++
		}
	}
	return nonEmpty >= 2
}

// Колонки, выровненные пробелами, выдаёт извлечение таблиц из PDF; отступ в начале не считается.
func hasMultiSpaceColumns(s string) bool {
	trimmed := strings.TrimLeft(s, " ")
	runStart := -1
	sawColumnBreak := false
	for idx, r := range trimmed {
		if r == ' ' {
			if runStart < 0 {
				runStart = idx
			}
			continue
		}
		if runStart >= 0 {
			if idx-runStart >= 2 {
				sawColumnBreak = true
				break
			}
			runStart = -1
		}
	}
	return sawColumnBreak
}

func attachCaptions(blocks []block) []block {
	out := make([]block, 0, len(blocks))
	for i := range blocks {
		b := blocks[i]
		if !isCaptionBlock(b) {
			out = append(out, b)
			continue
		}
		if n := len(out); n > 0 && out[n-1].kind == kindTable {
			out[n-1].text += b.text
			continue
		}
		if i+1 < len(blocks) {
			blocks[i+1].text = b.text + blocks[i+1].text
			if blocks[i+1].section == "" {
				blocks[i+1].section = b.section
			}
			continue
		}
		out = append(out, b)
	}
	return out
}

func isCaptionBlock(b block) bool {
	if b.kind != kindFlow {
		return false
	}
	t := strings.TrimSpace(b.text)
	return t != "" && !strings.ContainsRune(t, '\n') && isCaptionLine(t)
}

func isCaptionLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" || runeLen(t) > maxCaptionRunes {
		return false
	}
	lower := strings.ToLower(t)
	for _, p := range captionPrefixes() {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func captionPrefixes() []string {
	return []string{"рис.", "рисунок", "табл.", "таблица", "figure", "fig.", "table"}
}

func sectionHeading(line string) string {
	t := strings.TrimSpace(line)
	if t == "" || runeLen(t) > maxHeadingRunes {
		return ""
	}
	// Номер снимается целым токеном: посимвольная обрезка съела бы «I» из «Introduction» и «V» из «Validation».
	t = indexdoc.TrimHeadingNumber(t)
	t = strings.TrimRight(t, " .:—-：。、")
	word := firstWord(t)
	if word == "" {
		return ""
	}
	return canonicalSection(strings.ToLower(word))
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if unicode.IsSpace(r) {
			return s[:i]
		}
	}
	return s
}

func canonicalSection(word string) string {
	return displaySection(indexdoc.CanonicalSection(word))
}

// Словарь ключевых слов живёт только в indexdoc.CanonicalSection, чтобы подписи и канон не разошлись.
func displaySection(name string) string {
	return map[string]string{
		sourcedoc.SectionAbstract:         secAbstract,
		sourcedoc.SectionIntroduction:     secIntroduction,
		sourcedoc.SectionMethod:           secMethods,
		sourcedoc.SectionExperiments:      secExperiments,
		sourcedoc.SectionResults:          secResults,
		sourcedoc.SectionDiscussion:       secDiscussion,
		sourcedoc.SectionLimitations:      secLimitations,
		sourcedoc.SectionConclusion:       secConclusion,
		sourcedoc.SectionReferences:       secReferences,
		sourcedoc.SectionAcknowledgements: secAcknowledge,
		sourcedoc.SectionBackground:       secBackground,
		sourcedoc.SectionSummary:          secSummary,
		sourcedoc.SectionDescription:      secDescription,
		sourcedoc.SectionClaims:           secClaims,
	}[name]
}

func joinRaw(lines []line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.raw)
	}
	return b.String()
}
