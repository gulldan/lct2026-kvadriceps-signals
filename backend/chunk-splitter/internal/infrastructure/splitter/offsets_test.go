package splitter

import (
	"strings"
	"testing"
	"unicode"
)

func normForTest(s string) string {
	out := make([]rune, 0, len(s))
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			pendingSpace = true
			continue
		}
		if pendingSpace {
			if len(out) > 0 {
				out = append(out, ' ')
			}
			pendingSpace = false
		}
		out = append(out, r)
	}
	return string(out)
}

func sliceRunes(source string, start, end int) string {
	r := []rune(source)
	return string(r[start:end])
}

// Смесь абзацев, строк и двойных пробелов, чтобы схлопнутый чанк не был буквальной подстрокой;
// кириллица и тире разводят руны и байты.
const multiParaSource = "Первый  абзац содержит  двойные пробелы и тире — здесь.\n" +
	"Вторая строка того же абзаца продолжает мысль дальше.\n\n" +
	"Второй абзац начинается тут и тянется достаточно долго,\n" +
	"чтобы пересечь границу чанка и заставить overlap повториться.\n\n" +
	"Третий абзац короткий, но осмысленный."

func TestLocateOffsets_RealSpans_WithOverlap(t *testing.T) {
	// Малый размер и ненулевое перекрытие: головы чанков повторяют хвосты предыдущих — трудный случай
	// монотонного курсора.
	sp := NewRecursive(40, 12)
	chunks := sp.Split(multiParaSource)
	if len(chunks) < 3 {
		t.Fatalf("expected the source to split into several chunks, got %d", len(chunks))
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}

	offsets := LocateOffsets(multiParaSource, texts)
	if len(offsets) != len(chunks) {
		t.Fatalf("offsets len = %d, want %d", len(offsets), len(chunks))
	}

	srcRunes := len([]rune(multiParaSource))
	located := 0
	for i, off := range offsets {
		start, end := off[0], off[1]
		if start == 0 && end == 0 {
			// [0,0] допустим, но здесь каждый чанк должен находиться, поэтому промах попадает в лог.
			t.Logf("chunk %d (%q) reported unlocated [0,0]", i, texts[i])
			continue
		}
		located++

		if start < 0 || end > srcRunes || start >= end {
			t.Fatalf("chunk %d: span [%d,%d) out of bounds (source has %d runes)", i, start, end, srcRunes)
		}

		gotSpan := normForTest(sliceRunes(multiParaSource, start, end))
		wantChunk := normForTest(texts[i])
		if gotSpan != wantChunk {
			t.Fatalf("chunk %d span mismatch:\n  span  = %q\n  chunk = %q", i, gotSpan, wantChunk)
		}
	}

	if located == 0 {
		t.Fatal("no chunks were located; expected all real chunks to resolve")
	}
	if located != len(chunks) {
		t.Errorf("located %d of %d chunks; all should resolve for this source", located, len(chunks))
	}
}

func TestLocateOffsets_NotPresent_IsZeroZero(t *testing.T) {
	source := "alpha beta gamma delta epsilon"
	texts := []string{
		"beta gamma",                   // есть
		"this phrase is not in source", // нет → [0,0]
		"delta epsilon",                // есть, ПОСЛЕ отсутствующего
	}
	offsets := LocateOffsets(source, texts)

	if got := offsets[1]; got != [2]int{0, 0} {
		t.Fatalf("absent chunk should be [0,0], got %v", got)
	}
	// Отсутствующий чанк не должен сбить курсор: следующий найденный получает свой настоящий промежуток.
	for _, i := range []int{0, 2} {
		off := offsets[i]
		if off == ([2]int{0, 0}) {
			t.Fatalf("chunk %d (%q) should have located", i, texts[i])
		}
		if got := normForTest(sliceRunes(source, off[0], off[1])); got != normForTest(texts[i]) {
			t.Fatalf("chunk %d span %q != chunk %q", i, got, texts[i])
		}
	}
}

func TestLocateOffsets_Empty(t *testing.T) {
	if got := LocateOffsets("anything", nil); len(got) != 0 {
		t.Fatalf("nil chunks should yield empty offsets, got %v", got)
	}
	// Чанк из одних пробелов нормализуется в пустую строку и не находится: [0,0].
	got := LocateOffsets("some text here", []string{"   \n\t  "})
	if len(got) != 1 || got[0] != [2]int{0, 0} {
		t.Fatalf("whitespace-only chunk should be [0,0], got %v", got)
	}
}

func TestLocateOffsets_WindowBase(t *testing.T) {
	full := "PREFIX выкинутый текст. " + multiParaSource

	// Окно — всё от multiParaSource и дальше; его базовое смещение в full — длина выброшенного префикса в рунах.
	prefix := "PREFIX выкинутый текст. "
	baseRune := len([]rune(prefix))
	window := multiParaSource

	sp := NewRecursive(40, 12)
	chunks := sp.Split(window)
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}

	localOffsets := LocateOffsets(window, texts)

	fullRunes := len([]rune(full))
	checked := 0
	for i, lo := range localOffsets {
		if lo == ([2]int{0, 0}) {
			continue // не найден: [0,0] не сдвигается, как в absOffset
		}
		// Тот же сдвиг, что делает absOffset индексатора.
		absStart, absEnd := lo[0]+baseRune, lo[1]+baseRune
		if absStart < 0 || absEnd > fullRunes || absStart >= absEnd {
			t.Fatalf("chunk %d: absolute span [%d,%d) out of bounds (full has %d runes)", i, absStart, absEnd, fullRunes)
		}
		gotSpan := normForTest(sliceRunes(full, absStart, absEnd))
		wantChunk := normForTest(texts[i])
		if gotSpan != wantChunk {
			t.Fatalf("chunk %d absolute span mismatch:\n  span  = %q\n  chunk = %q", i, gotSpan, wantChunk)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no chunks checked against the full document; window-base coverage is vacuous")
	}
}

func TestLocateOffsetsShortChunkWhollyRepeatedByOverlap(t *testing.T) {
	// Короткий заголовок и почти полноразмерная аннотация дают два чанка; перекрытие в 150 рун
	// повторяет первый целиком, и оба начинаются с нуля.
	source := strings.Repeat("T", 75) + "\n\n" + strings.Repeat("a", 995)
	chunks := NewRecursive(1000, 150).Split(source)
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}
	offsets := LocateOffsets(source, texts)
	if len(offsets) != 2 || offsets[0][0] != 0 || offsets[1][0] != 0 || offsets[1][1] != len([]rune(source)) {
		t.Fatalf("overlap lost a legitimate shared start: %v", offsets)
	}
	for i, span := range offsets {
		if span == [2]int{0, 0} || normForTest(sliceRunes(source, span[0], span[1])) != normForTest(texts[i]) {
			t.Fatal("inexact anchor", i, span)
		}
	}
}

func TestLocateOffsetsRepeatedPassagesStillAdvance(t *testing.T) {
	offsets := LocateOffsets(strings.Join([]string{"same", "same", "same"}, " "), []string{"same", "same", "same"})
	want := [][2]int{{0, 4}, {5, 9}, {10, 14}}
	for i := range want {
		if offsets[i] != want[i] {
			t.Fatalf("repeated text reused an old span: %v", offsets)
		}
	}
}
