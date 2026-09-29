package pdf

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Пороги подобраны под научные, технические и юридические тексты на RU, EN и ZH.
const (
	maxHeadingRunes      = 140
	maxStandaloneRunes   = 80
	maxStandaloneWords   = 12
	minUppercaseFraction = 0.6
)

var (
	numberedHeadingRe = regexp.MustCompile(`^\s*\d+(\.\d+){0,4}\.?\s+\p{L}`)
	// RE2 \b знает только ASCII и после кириллицы не срабатывает, поэтому за ключевым словом
	// требуется не-буква или конец строки.
	keywordHeadingRe = regexp.MustCompile(`(?i)^\s*(` +
		`глава|раздел|часть|приложение|введение|заключение|аннотация|реферат|` +
		`список литературы|обозначения|chapter|section|part|appendix|` +
		`introduction|conclusion|abstract|references|` +
		`摘要|引言|绪论|实验方法|方法|实验|结果|讨论|结论|参考文献|致谢|§` +
		`)(?:[^\p{L}]|$)`)
	numberPrefixRe = regexp.MustCompile(`^\s*\d+(\.\d+){0,4}\.?\s+`)
	// Подписи рисунков и таблиц коротки и часто с заглавных — без вето прошли бы как заголовки.
	// [^\p{L}\d] вместо \W и \b работает и после кириллицы.
	captionLikeRe = regexp.MustCompile(`(?i)^\s*(` +
		`рис|рисунок|рисунке|рисунка|табл|таблица|таблице|таблицы|фиг|фигура|` +
		`схема|формула|уравнение|диаграмма|fig|figure|table|eq|equation|` +
		`scheme|chart|diagram|plate` +
		`)[^\p{L}\d]{0,3}\d`)
	// Короткая строка с годом — дата, титул или ссылка, а не заголовок раздела.
	yearRe = regexp.MustCompile(`\b(19|20)\d\d\b`)
)

// detectHeading намеренно консервативна: ложный section_heading портит провенанс чанков.
func detectHeading(paragraph string) bool {
	p := strings.TrimSpace(paragraph)
	if p == "" {
		return false
	}
	n := utf8.RuneCountInString(p)

	if captionLikeRe.MatchString(p) || yearRe.MatchString(p) {
		return false
	}

	if n <= maxHeadingRunes && numberedHeadingRe.MatchString(p) &&
		startsUpperAfterNumber(p) && !looksLikeSentence(p) {
		return true
	}

	if n <= maxHeadingRunes && keywordHeadingRe.MatchString(p) && !endsWithSentencePunct(p) {
		return true
	}

	if n <= maxStandaloneRunes && wordCount(p) <= maxStandaloneWords &&
		!endsWithSentencePunct(p) && !strings.ContainsAny(p, ",;") {
		if mostlyUppercase(p) {
			return true
		}
		if titleCase(p) && letterWords(p) >= 2 {
			return true // одиночное слово с заглавной — чаще имя собственное
		}
	}

	return false
}

// Отсекает даты вида «12 марта 2024» (месяц строчный); у CJK нет регистра, поэтому проверка
// «буква и не строчная», а не IsUpper.
func startsUpperAfterNumber(p string) bool {
	body := numberPrefixRe.ReplaceAllString(p, "")
	r, _ := utf8.DecodeRuneInString(body)
	return unicode.IsLetter(r) && !unicode.IsLower(r)
}

func letterWords(p string) int {
	n := 0
	for _, w := range strings.Fields(p) {
		if r, _ := utf8.DecodeRuneInString(w); unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

func looksLikeSentence(p string) bool {
	if utf8.RuneCountInString(p) > maxStandaloneRunes && wordCount(p) > maxStandaloneWords {
		return true
	}
	// Точка в конце допустима («1. Введение»), но вместе с запятой это уже фраза.
	if endsWithSentencePunct(p) {
		if strings.ContainsRune(p, ',') {
			return true
		}
	}
	// Номер раздела срезается заранее, чтобы точки в «2.3.1» не сочлись концом предложения.
	body := numberPrefixRe.ReplaceAllString(p, "")
	if i := strings.IndexByte(body, '.'); i >= 0 && i+1 < len(body) {
		rest := strings.TrimSpace(body[i+1:])
		if strings.ContainsRune(rest, ' ') && wordCount(rest) >= 3 {
			return true
		}
	}
	return false
}

func endsWithSentencePunct(p string) bool {
	r, _ := utf8.DecodeLastRuneInString(p)
	switch r {
	case '.', '!', '?', ',', ';', ':', '。', '！', '？', '，', '；', '：':
		return true
	default:
		return false
	}
}

func wordCount(p string) int { return len(strings.Fields(p)) }

func mostlyUppercase(p string) bool {
	var letters, upper int
	for _, r := range p {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsUpper(r) {
			upper++
		}
	}
	if letters == 0 {
		return false
	}
	return float64(upper)/float64(letters) >= minUppercaseFraction
}

func titleCase(p string) bool {
	var words, capped int
	for _, w := range strings.Fields(p) {
		r, _ := utf8.DecodeRuneInString(w)
		if !unicode.IsLetter(r) {
			continue
		}
		words++
		if unicode.IsUpper(r) {
			capped++
		}
	}
	if words == 0 {
		return false
	}
	return capped*2 > words
}

func leadingSpaceRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			break
		}
		n++
	}
	return n
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
