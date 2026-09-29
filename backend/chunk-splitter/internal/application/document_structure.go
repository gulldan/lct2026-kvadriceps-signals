package application

import (
	"encoding/json"
	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/sourcedoc"
	"sort"
	"strings"
	"unicode/utf8"
)

// Нулевое значение сводит все методы к 0 и "", и индексация идёт как без структуры.
type docStructure struct {
	pageRuneStarts []int // начало каждой страницы в рунах: [0] == 0, строго растут
	pageByteStarts []int // те же границы в байтах — для окон по страницам
	sections       []sectionMark
}

type sectionMark struct {
	rune    int
	heading string
}

// Битые или выходящие за текст метаданные игнорируются: плохой продюсер не должен портить индексацию.
func parseDocStructure(meta map[string]string, text string) docStructure {
	var ds docStructure
	if meta == nil {
		return ds
	}
	runeLen := utf8.RuneCountInString(text)
	if raw := meta["page_offsets"]; raw != "" {
		var offs []int
		if err := json.Unmarshal([]byte(raw), &offs); err == nil && validPageOffsets(offs, runeLen) {
			ds.pageRuneStarts = offs
			ds.pageByteStarts = byteStartsOf(text, offs)
		}
	}
	if raw := meta["section_offsets"]; raw != "" {
		var marks []struct {
			Rune    int    `json:"rune"`
			Heading string `json:"heading"`
		}
		if err := json.Unmarshal([]byte(raw), &marks); err == nil {
			for _, m := range marks {
				if h := strings.TrimSpace(m.Heading); m.Rune >= 0 && m.Rune <= runeLen && h != "" {
					ds.sections = append(ds.sections, sectionMark{rune: m.Rune, heading: h})
				}
			}
			sort.Slice(ds.sections, func(i, j int) bool { return ds.sections[i].rune < ds.sections[j].rune })
		}
	}
	return ds
}

func validPageOffsets(offs []int, runeLen int) bool {
	if len(offs) == 0 || offs[0] != 0 {
		return false
	}
	for i := 1; i < len(offs); i++ {
		if offs[i] <= offs[i-1] || offs[i] > runeLen {
			return false
		}
	}
	return true
}

func byteStartsOf(text string, runeStarts []int) []int {
	out := make([]int, 0, len(runeStarts))
	ri, next := 0, 0
	for bi := range text {
		for next < len(runeStarts) && runeStarts[next] == ri {
			out = append(out, bi)
			next++
		}
		ri++
	}
	for next < len(runeStarts) {
		out = append(out, len(text))
		next++
	}
	return out
}

func (ds docStructure) pageOf(r int) int {
	if len(ds.pageRuneStarts) == 0 {
		return 0
	}
	if r < 0 {
		r = 0
	}
	i := sort.Search(len(ds.pageRuneStarts), func(i int) bool { return ds.pageRuneStarts[i] > r }) - 1
	if i < 0 {
		i = 0
	}
	return i + 1
}

func (ds docStructure) sectionAt(r int) string {
	h := ""
	for _, m := range ds.sections {
		if m.rune > r {
			break
		}
		h = m.heading
	}
	return h
}

// Парсер без структуры не значит документ без структуры, поэтому запасной путь — поиск заголовков по блокам.
func structureOf(evt *commonv1.DocumentParsed, text string) docStructure {
	ds := parseDocStructure(evt.GetMetadata(), text)
	if len(ds.sections) == 0 {
		for _, mark := range splitter.SectionMarks(text) {
			ds.sections = append(ds.sections, sectionMark{rune: mark.Rune, heading: mark.Heading})
		}
	}
	return ds
}

// Раздел либо адресуем, либо отсутствует: заголовки вне общего словаря и пустые промежутки выбрасываются.
func (ds docStructure) spans(runeLen int) []sourcedoc.Section {
	sections := []sourcedoc.Section{}
	for i, mark := range ds.sections {
		name := indexdoc.CanonicalSection(mark.heading)
		end := runeLen
		if i+1 < len(ds.sections) {
			end = ds.sections[i+1].rune
		}
		if name == "" || mark.rune < 0 || end <= mark.rune || end > runeLen {
			continue
		}
		sections = append(sections, sourcedoc.Section{Name: name, Title: mark.heading, Start: mark.rune, End: end})
	}
	if sourcedoc.ValidateSections(sections, runeLen) != nil {
		return nil
	}
	return sections
}

// -1 без структуры страниц, чтобы вызывающий не трогал окна; textLen — если границ дальше нет.
func (ds docStructure) nextPageByteAfter(start, textLen int) int {
	if len(ds.pageByteStarts) == 0 {
		return -1
	}
	for _, b := range ds.pageByteStarts {
		if b > start {
			return b
		}
	}
	return textLen
}
