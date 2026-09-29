package indexdoc

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/sourcedoc"
)

const patentText = "Способ получения водорода электролизом воды на протонообменной мембране."

func TestLocatorReproducesItsQuote(t *testing.T) {
	text := []rune(patentText)
	quote := "протонообменной мембране"
	at := strings.Index(patentText, quote)
	if at < 0 {
		t.Fatal("the fixture does not contain the quote")
	}
	start := utf8.RuneCountInString(patentText[:at])
	locator := Locator{Section: "claims", StartChar: start, EndChar: start + len([]rune(quote)),
		ElementPath: "claim/1", SnapshotSHA256: Hash([]byte(patentText))}
	if err := locator.Verify(text, quote); err != nil {
		t.Fatalf("an exact locator was refused: %v", err)
	}
	for name, broken := range map[string]Locator{
		"shifted by one": {StartChar: locator.StartChar + 1, EndChar: locator.EndChar},
		"past the end":   {StartChar: locator.StartChar, EndChar: len(text) + 1},
		"empty span":     {StartChar: locator.StartChar, EndChar: locator.StartChar},
		"negative start": {StartChar: -1, EndChar: 4},
		"reversed":       {StartChar: locator.EndChar, EndChar: locator.StartChar},
		"whole document": {StartChar: 0, EndChar: len(text)},
	} {
		if broken.Verify(text, quote) == nil {
			t.Fatalf("a locator that does not reproduce its quote was accepted: %s", name)
		}
	}
}

func TestCanonicalSectionFoldsHeadings(t *testing.T) {
	for heading, want := range map[string]string{
		"Abstract": "abstract", "Аннотация": "abstract", "摘要": "abstract",
		"Introduction": "introduction", "Введение": "introduction",
		"Methods": "method", "Методика": "method", "METHOD": "method",
		"Experiments": "experiments", "Experimental setup": "experiments", "Evaluation": "experiments",
		"Limitations": "limitations", "Ограничения": "limitations",
		"References": "references", "Литература": "references",
		"Background": "background", "Summary": "summary", "Description": "description", "Claims": "claims",
		"2. Methods": "method", "II. Method": "method", "3 Experiments": "experiments",
		"1 Introduction": "introduction", "5. Limitations and future work": "limitations",
		"一、方法": "method",
	} {
		if got := CanonicalSection(heading); got != want {
			t.Fatalf("heading %q folded to %q, want %q", heading, got, want)
		}
	}
	for _, heading := range []string{
		"", "Приложение А", "Figure 3", "Таблица 1", "unrelated",
		"3 Denoising in LLM-Oriented Retrieval", "7 M", "Structure-aware Indexing",
	} {
		if got := CanonicalSection(heading); got != "" {
			t.Fatalf("heading %q was guessed into section %q", heading, got)
		}
	}
}

func TestSectionsMustAddressTheStoredText(t *testing.T) {
	runes := len([]rune(patentText))
	good := []sourcedoc.Section{{Name: "abstract", Start: 0, End: 20}, {Name: "claims", Start: 20, End: runes}}
	if err := sourcedoc.ValidateSections(good, runes); err != nil {
		t.Fatalf("an addressable structure was refused: %v", err)
	}
	for name, broken := range map[string][]sourcedoc.Section{
		"overlapping":   {{Name: "abstract", Start: 0, End: 30}, {Name: "claims", Start: 20, End: runes}},
		"out of order":  {{Name: "claims", Start: 20, End: runes}, {Name: "abstract", Start: 0, End: 20}},
		"past the text": {{Name: "abstract", Start: 0, End: runes + 1}},
		"empty":         {{Name: "abstract", Start: 5, End: 5}},
		"unknown name":  {{Name: "appendix", Start: 0, End: 10}},
	} {
		if sourcedoc.ValidateSections(broken, runes) == nil {
			t.Fatalf("an unaddressable structure was accepted: %s", name)
		}
	}
}
