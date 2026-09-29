package splitter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Короткие разреженные последовательности («a b c d», «И. И.») не трогаются. Промежуток до maxConnectorGap
// пробелов допустим, потому что экстракторы PDF мешают \n и \n\n между глифами; каждый токен — одна буква.
const (
	minTrackedRun   = 5
	maxConnectorGap = 2
)

// CollapseTracking склеивает разреженный набор из PDF («М е т о д» → «Метод»). Ручной проход по рунам,
// потому что в RE2 нет lookahead; всё вне склеенных участков выводится байт в байт, так что функция идемпотентна.
func CollapseTracking(s string) string {
	if s == "" {
		return s
	}

	tokens, gaps := tokenize(s)
	if len(tokens) == 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(gaps[0])

	i := 0
	for i < len(tokens) {
		runLen := trackedRunLength(tokens, gaps, i)
		if runLen >= minTrackedRun {
			for j := i; j < i+runLen; j++ {
				b.WriteString(tokens[j])
			}
			b.WriteString(gaps[i+runLen])
			i += runLen
			continue
		}
		b.WriteString(tokens[i])
		b.WriteString(gaps[i+1])
		i++
	}
	return b.String()
}

// len(gaps) == len(tokens)+1, и чередование точно восстанавливает s.
func tokenize(s string) (tokens []string, gaps []string) {
	gaps = append(gaps, "")
	tokStart := -1
	gapStart := 0

	for i, r := range s {
		if unicode.IsSpace(r) {
			if tokStart >= 0 {
				tokens = append(tokens, s[tokStart:i])
				tokStart = -1
				gapStart = i
			}
			continue
		}
		if tokStart < 0 {
			if len(tokens) == 0 {
				gaps[0] = s[gapStart:i]
			} else {
				gaps = append(gaps, s[gapStart:i])
			}
			tokStart = i
		}
	}

	if tokStart >= 0 {
		tokens = append(tokens, s[tokStart:])
		gaps = append(gaps, "") // после последнего токена промежутка нет
	} else {
		if len(tokens) == 0 {
			gaps[0] = s[gapStart:] // вход из одних пробелов
		} else {
			gaps = append(gaps, s[gapStart:])
		}
	}
	return tokens, gaps
}

func trackedRunLength(tokens, gaps []string, i int) int {
	if !isSingleLetter(tokens[i]) {
		return 0
	}
	n := 1
	for i+n < len(tokens) {
		// gaps[i+n] — промежуток МЕЖДУ tokens[i+n-1] и tokens[i+n].
		if !isConnectorGap(gaps[i+n]) || !isSingleLetter(tokens[i+n]) {
			break
		}
		n++
	}
	return n
}

func isSingleLetter(tok string) bool {
	if utf8.RuneCountInString(tok) != 1 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(tok)
	return unicode.IsLetter(r)
}

// Руны считать можно: tokenize кладёт в промежуток только пробельные символы.
func isConnectorGap(gap string) bool {
	n := utf8.RuneCountInString(gap)
	return n >= 1 && n <= maxConnectorGap
}
