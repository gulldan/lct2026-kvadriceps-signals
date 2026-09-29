package splitter

import (
	"strings"
	"testing"
)

func TestTableCaptionConsumedExactlyOnce(t *testing.T) {
	text := "Material | Value\nLiFePO4 | 3.4\nLiCoO2 | 4.2\nТаблица 1 — результат\nFollowing paragraph."
	var reconstructed strings.Builder
	for _, block := range segment(text) {
		reconstructed.WriteString(block.text)
	}
	if reconstructed.String() != text {
		t.Fatal("structural segmentation duplicated or reordered source text")
	}
}

func TestTrailingTableWhitespaceDoesNotRepeatItsOverlap(t *testing.T) {
	text := "Material | Value\nLiFePO4 | 3.4\nLiCoO2 | 4.2\n\n\n"
	chunks := NewRecursive(1000, 150).Split(text)
	if len(chunks) != 1 {
		t.Fatalf("blank structural gap emitted a second copy of the table: %d chunks", len(chunks))
	}
}

func chunkHolding(chunks []chunkText, markers ...string) int {
	for i, c := range chunks {
		all := true
		for _, m := range markers {
			if !strings.Contains(c.text, m) {
				all = false
				break
			}
		}
		if all {
			return i
		}
	}
	return -1
}

type chunkText struct {
	text    string
	section string
}

func split(size, overlap int, doc string) []chunkText {
	src := NewRecursive(size, overlap).Split(doc)
	out := make([]chunkText, 0, len(src))
	for _, c := range src {
		out = append(out, chunkText{text: c.Text, section: c.Section})
	}
	return out
}

func TestSplit_TableBlockStaysIntact(t *testing.T) {
	// Без защиты таблиц рекурсивные разделители разрезали бы эти строки по \n.
	pipeTable := "Состав | Температура | Свойство\n" +
		"LiFePO4 | 800 C | 120 mAh/g\n" +
		"TiO2 | 950 C | 85 mAh/g\n" +
		"Al-Mg-Si | 530 C | 310 MPa\n"
	spaceTable := "Состав      Температура      Свойство\n" +
		"LiFePO4      800 C      120 mAh/g\n" +
		"TiO2      950 C      85 mAh/g\n" +
		"Al-Mg-Si      530 C      310 MPa\n"

	tests := []struct {
		name  string
		table string
	}{
		{name: "pipe-separated", table: pipeTable},
		{name: "multi-space columns", table: spaceTable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := "Введение в исследование катодных материалов и их характеристик весьма подробно.\n\n" +
				tc.table +
				"\nПосле таблицы следует обсуждение результатов и краткие выводы по работе автора."
			// Размер 80 меньше таблицы, и незащищённый сплиттер её разрезал бы; в предел превышения 3x
			// она укладывается и остаётся целой.
			chunks := split(80, 12, doc)
			if got := len([]rune(tc.table)); got <= 80 {
				t.Fatalf("test table too short (%d runes) to exceed size 80", got)
			}
			idx := chunkHolding(chunks, "LiFePO4", "TiO2", "Al-Mg-Si")
			if idx < 0 {
				for i, c := range chunks {
					t.Logf("chunk %d: %q", i, c.text)
				}
				t.Fatal("table rows were split across chunks; expected all rows in one chunk")
			}
		})
	}
}

func TestSplit_CaptionStaysWithBlock(t *testing.T) {
	table := "LiFePO4 | 800 C | 120\nTiO2 | 950 C | 85\nAl-Mg-Si | 530 C | 310\n"
	tests := []struct {
		name    string
		doc     string
		caption string
	}{
		{
			name: "caption below table",
			doc: "Преамбула раздела с описанием эксперимента и условий синтеза образцов подробно.\n\n" +
				table +
				"Таблица 1 — Свойства образцов.\n\n" +
				"Текст после таблицы и подписи продолжает изложение материала статьи дальше тут.",
			caption: "Таблица 1",
		},
		{
			name: "caption above table",
			doc: "Преамбула раздела с описанием эксперимента и условий синтеза образцов подробно.\n\n" +
				"Таблица 2 — Состав образцов.\n" +
				table +
				"\nТекст после таблицы продолжает изложение материала статьи дальше по разделу.",
			caption: "Таблица 2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chunks := split(80, 12, tc.doc)
			if chunkHolding(chunks, tc.caption, "LiFePO4") < 0 {
				for i, c := range chunks {
					t.Logf("chunk %d: %q", i, c.text)
				}
				t.Fatalf("caption %q was separated from its table", tc.caption)
			}
		})
	}
}

func TestSplit_SectionHeadingTagsChunks(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		want    string
	}{
		{name: "english methods", heading: "Methods", want: "Methods"},
		{name: "russian results numbered", heading: "3. Результаты", want: "Results"},
		{name: "english conclusion colon", heading: "Conclusion:", want: "Conclusion"},
		{name: "not a heading", heading: "Краткое введение в проблему", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := tc.heading + "\n\n" +
				"Тело раздела с достаточным количеством слов чтобы получить несколько чанков точно.\n\n" +
				"Второй абзац раздела добавляет ещё немного текста для второго чанка наверняка здесь."
			chunks := split(60, 10, doc)
			if len(chunks) < 2 {
				t.Fatalf("expected multiple chunks, got %d", len(chunks))
			}
			tagged := 0
			for _, c := range chunks {
				if c.section == tc.want {
					tagged++
				}
			}
			if tc.want == "" {
				if tagged != len(chunks) {
					t.Fatalf("non-heading must leave section empty on all chunks, got %d/%d empty", tagged, len(chunks))
				}
				return
			}
			if tagged == 0 {
				t.Fatalf("no chunk tagged with section %q", tc.want)
			}
		})
	}
}

func TestNumberedHeadingsAreRecognised(t *testing.T) {
	for heading, want := range map[string]string{
		"1 Introduction": secIntroduction, "2. Methods": secMethods, "II. Method": secMethods,
		"3) Experiments": secExperiments, "5. Limitations": secLimitations,
		"一、方法": secMethods, "Введение": secIntroduction, "4 Результаты": secResults,
		"Introduction": secIntroduction, "References": secReferences,
	} {
		if got := sectionHeading(heading); got != want {
			t.Fatalf("heading %q resolved to %q, want %q", heading, got, want)
		}
	}
	for _, heading := range []string{"3 Denoising in LLM-Oriented Retrieval", "7 M", "Structure-aware Indexing"} {
		if got := sectionHeading(heading); got != "" {
			t.Fatalf("heading %q was guessed into section %q", heading, got)
		}
	}
	marks := SectionMarks("1 Introduction\nТекст.\n2. Methods\nЕщё текст.\n")
	if len(marks) != 2 || marks[0].Heading != secIntroduction || marks[1].Heading != secMethods {
		t.Fatalf("numbered headings were not located: %+v", marks)
	}
	if marks[0].Rune != 0 || marks[1].Rune != 22 {
		t.Fatalf("section marks are at %d and %d", marks[0].Rune, marks[1].Rune)
	}
}
