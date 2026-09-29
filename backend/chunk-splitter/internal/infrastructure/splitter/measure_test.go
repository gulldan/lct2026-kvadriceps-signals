package splitter

// Поддельная мера считает слова, а не руны: проверка бюджета проходит только через внедрённую меру.

import (
	"strings"
	"testing"
)

func wordCount(s string) int { return len(strings.Fields(s)) }

func TestNewRecursiveWithMeasure_HonoursWordBudget(t *testing.T) {
	const budget = 8 // слов на чанк
	sp := NewRecursiveWithMeasure(budget, 5 /*перекрытие в рунах*/, wordCount)

	text := strings.Join([]string{
		"alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi.",
		"Второй абзац содержит несколько слов и продолжает мысль дальше по тексту примерно.",
		"",
		"Третий абзац короткий но добавляет ещё немного слов чтобы было несколько чанков точно.",
	}, "\n\n")

	chunks := sp.Split(text)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks for a multi-word document, got %d", len(chunks))
	}

	for i, c := range chunks {
		// У следующих чанков префикс перекрытия в рунах может превысить бюджет, поэтому целиком проверяется только нулевой.
		if i == 0 {
			if got := wordCount(c.Text); got > budget {
				t.Fatalf("chunk 0 measured %d words > budget %d: %q", got, budget, c.Text)
			}
		}
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	offsets := LocateOffsets(text, texts)
	if len(offsets) != len(chunks) {
		t.Fatalf("offsets len %d != chunks %d", len(offsets), len(chunks))
	}
	located := 0
	srcRunes := len([]rune(text))
	for i, off := range offsets {
		if off == ([2]int{0, 0}) {
			t.Logf("chunk %d unlocated: %q", i, texts[i])
			continue
		}
		if off[0] < 0 || off[1] > srcRunes || off[0] >= off[1] {
			t.Fatalf("chunk %d span %v out of bounds (%d runes)", i, off, srcRunes)
		}
		if got, want := normForTest(sliceRunes(text, off[0], off[1])), normForTest(texts[i]); got != want {
			t.Fatalf("chunk %d span does not reproduce chunk:\n  span  = %q\n  chunk = %q", i, got, want)
		}
		located++
	}
	if located != len(chunks) {
		t.Fatalf("located %d of %d chunks; word sizing must not break provenance", located, len(chunks))
	}
}

func TestNewRecursiveWithMeasure_MergeBodyNeverExceedsBudget(t *testing.T) {
	const (
		budget  = 6
		overlap = 0 // без перекрытия чанк равен телу склейки
	)
	sp := NewRecursiveWithMeasure(budget, overlap, wordCount)

	text := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen"
	chunks := sp.Split(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if got := wordCount(c.Text); got > budget {
			t.Fatalf("chunk %d measured %d words > budget %d (overlap=0): %q", i, got, budget, c.Text)
		}
	}
}

// Единственный допустимый выход за бюджет: неделимый кусок без разделителя становится отдельным чанком,
// а не зацикливается и не теряется.
func TestNewRecursiveWithMeasure_OversizedSinglePieceEdge(t *testing.T) {
	// Мера в рунах: блоб без пробелов — одно слово, и бюджет в словах он бы не превысил.
	bigMeasure := func(s string) int { return len([]rune(s)) }
	const budget = 10
	sp := NewRecursiveWithMeasure(budget, 0, bigMeasure)

	blob := strings.Repeat("z", 35) // 35 рун без разделителя — один кусок сверх бюджета
	chunks := sp.Split(blob)
	if len(chunks) == 0 {
		t.Fatal("oversized blob produced no chunks")
	}
	// hardSplit режет блоб на окна по бюджету; важно, что он завершается и восстанавливает исходник.
	joined := strings.Join(func() []string {
		out := make([]string, len(chunks))
		for i, c := range chunks {
			out[i] = c.Text
		}
		return out
	}(), "")
	if joined != blob {
		t.Fatalf("oversized blob not reconstructed: got %q want %q", joined, blob)
	}
}
