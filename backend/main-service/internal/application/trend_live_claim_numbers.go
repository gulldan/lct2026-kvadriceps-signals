package application

import (
	"regexp"
	"strings"
	"unicode"
)

const liveEnglishNumberWords = "zero|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|" +
	"fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|" +
	"hundred|thousand|million|billion|and|point|half|halves|third|thirds|quarter|quarters"

const liveEnglishDenominators = "fourth|fifth|sixth|seventh|eighth|ninth|tenth|eleventh|twelfth|thirteenth|" +
	"fourteenth|fifteenth|sixteenth|seventeenth|eighteenth|nineteenth|twentieth|thirtieth|fortieth|fiftieth|" +
	"sixtieth|seventieth|eightieth|ninetieth|hundredth|thousandth|millionth|billionth"

var (
	liveNumberWords = regexp.MustCompile(`(?i)\b(?:` + liveEnglishNumberWords + `)(?:[[:space:]‐‑–/-]+(?:` +
		liveEnglishNumberWords + `))*\b`)
	liveWrittenNumberBefore = regexp.MustCompile(`(?i)(?:\b(?:minus|negative)|[-−‐‑–/⁄])\s*$`)
	liveWrittenNumberAfter  = regexp.MustCompile(`(?i)^\s*(?:[-−‐‑–/⁄]|(?:` + liveEnglishDenominators + `)s?\b)`)
)

// Словесная запись числа не должна терять допустимый русский пересказ: «девяносто шесть» и 96 равнозначны.
// Разбираются только однозначные целые до ста; части сотен, дробей и перечислений не становятся отдельными числами.
// Исходная цитата остаётся неизменной, а связь числа с опытом по-прежнему проверяет независимая модель.
func liveWrittenNumbers(quote string) []float64 {
	values := map[string]int{
		"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
		"ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
		"seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20, "thirty": 30, "forty": 40, "fifty": 50,
		"sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
	}
	var numbers []float64
	for _, bounds := range liveNumberWords.FindAllStringIndex(quote, -1) {
		phrase := strings.ToLower(quote[bounds[0]:bounds[1]])
		if strings.Contains(phrase, "/") || liveWrittenNumberBefore.MatchString(quote[:bounds[0]]) ||
			liveWrittenNumberAfter.MatchString(quote[bounds[1]:]) {
			continue
		}
		words := strings.FieldsFunc(phrase, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("-‐‑–/", r) })
		first, known := values[words[0]]
		if !known {
			continue
		}
		switch len(words) {
		case 1:
			numbers = append(numbers, float64(first))
		case 2:
			last, known := values[words[1]]
			if known && first >= 20 && first%10 == 0 && last > 0 && last < 10 {
				numbers = append(numbers, float64(first+last))
			}
		}
	}
	return numbers
}
