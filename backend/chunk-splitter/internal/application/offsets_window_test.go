package application

// Путь смещений через несколько окон (start > 0, непустой carry) тесты сплиттера с одним окном не покрывают.

import (
	"strings"
	"testing"
	"unicode"

	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
)

// normWS повторяет нормализацию пробелов сплиттера и LocateOffsets.
func normWS(s string) string {
	out := make([]rune, 0, len(s))
	pending := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			pending = true
			continue
		}
		if pending {
			if len(out) > 0 {
				out = append(out, ' ')
			}
			pending = false
		}
		out = append(out, r)
	}
	return string(out)
}

func sliceRunesAbs(source string, start, end int) string {
	r := []rune(source)
	return string(r[start:end])
}

func TestWindowedOffsets_MultiWindowCarry(t *testing.T) {
	// Кириллица (2 байта на руну), чтобы скромное окно в байтах дало несколько окон.
	full := strings.Join([]string{
		"Первый абзац о буровых растворах содержит двойные  пробелы и тире — вот так.",
		"Вторая строка того же абзаца продолжает мысль и тянется ещё дальше по тексту.",
		"",
		"Второй абзац начинается здесь и описывает коррозию насосно-компрессорных труб,",
		"чтобы заведомо пересечь границу окна и заставить overlap-хвост повториться.",
		"",
		"Третий абзац рассказывает про композитные материалы и температурную стойкость,",
		"добавляя ещё немного длины, чтобы окон точно стало несколько подряд.",
		"",
		"Четвёртый абзац завершает документ короткой, но осмысленной фразой о результатах.",
	}, "\n")

	const (
		window  = 140 // байты; мало, чтобы окон было несколько
		overlap = 12  // руны
	)
	sp := splitter.NewRecursive(40, overlap)

	var (
		carry   string
		windows int
		checked int
	)
	for start := 0; start < len(full); {
		end := windowEnd(full, start, window)
		seg := carry + full[start:end]
		chunks := sp.Split(seg)
		offsets := splitter.LocateOffsets(seg, chunkTexts(chunks))
		baseRune := windowBaseRune(full, start, carry)

		for _, c := range chunks {
			cs, ce := absOffset(offsets, c.Index, baseRune)
			if cs == 0 && ce == 0 {
				continue // не найден: не сдвигается, проверять нечего
			}
			if cs < 0 || ce > len([]rune(full)) || cs >= ce {
				t.Fatalf("window@%d chunk %d: absolute span [%d,%d) out of bounds", start, c.Index, cs, ce)
			}
			if got, want := normWS(sliceRunesAbs(full, cs, ce)), normWS(c.Text); got != want {
				t.Fatalf("window@%d chunk %d: absolute span does not reproduce chunk\n  span  = %q\n  chunk = %q", start, c.Index, got, want)
			}
			checked++
		}
		carry = tailRunes(seg, overlap)
		start = end
		windows++
	}

	if windows < 2 {
		t.Fatalf("test must span >=2 windows to exercise the carry path, got %d", windows)
	}
	if checked == 0 {
		t.Fatal("no located spans were verified; coverage is vacuous")
	}
	t.Logf("verified %d absolute spans across %d windows", checked, windows)
}
