package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWebAnchorsReproduceTheirBlocks(t *testing.T) {
	page := "<html><body><nav>Меню</nav><article>" +
		"<h2>Метод</h2>" +
		"<p>Электролизёр на протонообменной мембране работал тысячу часов.</p>" +
		"<p>Перовскитный модуль сохранил эффективность после термоциклирования.</p>" +
		"</article><footer>© Лаборатория</footer></body></html>"
	text, anchors := textHTMLAnchored(page)
	if len(anchors) != len(textBlocks(text)) {
		t.Fatalf("anchors and blocks are not parallel: %d anchors, %d blocks", len(anchors), len(textBlocks(text)))
	}
	runes := []rune(text)
	if len(runes) == len(text) {
		t.Fatal("the fixture must be multi-byte, otherwise it proves nothing about the unit")
	}
	paths := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		if anchor.End > len(runes) || anchor.End <= anchor.Start {
			t.Fatalf("anchor %+v is not addressable in the extracted text", anchor)
		}
		quote := string(runes[anchor.Start:anchor.End])
		if !strings.Contains(text, quote) {
			t.Fatalf("anchor %s does not reproduce its block", anchor.Path)
		}
		paths = append(paths, anchor.Path)
	}
	if !strings.Contains(strings.Join(paths, " "), "/p[2]") {
		t.Fatalf("an element path does not name the element it came from: %v", paths)
	}
	if strings.Contains(text, "Меню") || strings.Contains(text, "Лаборатория") {
		t.Fatal("navigation and footer entered the extracted text")
	}
	if body := textHTML(page); body != text {
		t.Fatal("the anchored extractor and the plain one disagree about the text")
	}
}

func TestWebAnchorsFollowBoilerplateRemoval(t *testing.T) {
	shared := "Материал подготовлен пресс-службой лаборатории."
	data := &snapshot{Records: []record{}}
	for i, body := range []string{
		"Электролизёр на мембране работал тысячу часов.",
		"Перовскитный модуль сохранил эффективность.",
		"Натриевый аккумулятор выдержал циклирование.",
	} {
		page := "<html><body><article><p>" + shared + "</p><p>" + body + "</p></article></body></html>"
		text, anchors := textHTMLAnchored(page)
		data.Records = append(data.Records, record{
			ID: string(rune('a' + i)), URL: "https://lab.example/" + string(rune('a'+i)),
			Text: text, Anchors: anchors, TextStatus: articleText,
		})
	}
	dropWebBoilerplate(data)
	for _, row := range data.Records {
		if strings.Contains(row.Text, shared) {
			t.Fatalf("the repeated block stayed in %s", row.URL)
		}
		runes := []rune(row.Text)
		if len(row.Anchors) != len(textBlocks(row.Text)) {
			t.Fatalf("%s kept %d anchors for %d blocks", row.URL, len(row.Anchors), len(textBlocks(row.Text)))
		}
		for _, anchor := range row.Anchors {
			if anchor.End > len(runes) || string(runes[anchor.Start:anchor.End]) != textBlocks(row.Text)[0] {
				t.Fatalf("anchor %+v of %s no longer names its own block", anchor, row.URL)
			}
		}
		if utf8.RuneCountInString(row.Text) != len(runes) {
			t.Fatal("stored text and its code points disagree")
		}
	}
}
