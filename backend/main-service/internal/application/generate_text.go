package application

import (
	"strings"
	"unicode/utf8"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const perovskiteSolar = "perovskite solar"

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func sourceTitle(src *commonv1.Source) string {
	filename := compactText(src.GetFilename())
	if filename != "" {
		if base, _, found := strings.CutLast(filename, "."); found && base != "" {
			filename = base
		}
		filename = strings.TrimLeft(filename, "0123456789_-. ")
		filename = strings.ReplaceAll(filename, "_", " ")
		filename = strings.ReplaceAll(filename, "-", " ")
		return compactText(filename)
	}
	return ""
}

func validHypothesisTitle(title string) bool {
	lower := strings.ToLower(compactText(title))
	if lower == "" {
		return false
	}
	noisy := []string{
		"деньги",
		"выплавик",
		"анализ астрономических наблюдений",
		"моделирование нефтяных резервуаров",
		"corporate source",
		"star accordance",
		"bicep",
		"keck",
		"trieste italy",
		"indian ropar",
		"boolean variables",
		perovskiteSolar,
		"использование решения «article",
		"использование решения «ntrs",
	}
	for _, fragment := range noisy {
		if strings.Contains(lower, fragment) {
			return false
		}
	}
	prefixes := []string{
		"проверить ", "проверь ", "оценить ", "исследовать ", "изучить ",
		"проанализировать ", "рассмотреть ", "сформировать ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	if strings.Contains(lower, " для kpi") && strings.Contains(lower, "подход") {
		return false
	}
	return true
}

func hypothesisTitleKey(title string) string {
	return strings.ToLower(compactText(title))
}

func compactText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstSentence(s string, limit int) string {
	s = compactText(s)
	if s == "" || limit <= 0 {
		return s
	}
	cut := utf8.RuneCountInString(s)
	for _, sep := range []string{". ", "。", "! ", "? ", "\n"} {
		if idx := strings.Index(s, sep); idx > 0 {
			runeIdx := utf8.RuneCountInString(s[:idx+len(sep)])
			if runeIdx < cut {
				cut = runeIdx
			}
		}
	}
	if cut > limit {
		cut = limit
	}
	runes := []rune(s)
	out := strings.TrimSpace(string(runes[:cut]))
	truncated := utf8.RuneCountInString(out) < len(runes)
	if truncated && !strings.HasSuffix(out, ".") && !strings.HasSuffix(out, "!") && !strings.HasSuffix(out, "?") {
		out = strings.TrimRight(out, " ,;:") + "..."
	}
	return out
}
