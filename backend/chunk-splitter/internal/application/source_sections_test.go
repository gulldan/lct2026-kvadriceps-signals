package application

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/example/chunk-splitter/internal/platform/contracts"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/sourcedoc"
)

// PDF сообщает смещения разделов, патентный XML и веб-страница приходят с разделами и локаторами
// сборщика, скан OCR — без того и другого.
type sectionedInput struct {
	name     string
	source   string
	text     string
	metadata map[string]string
	sections []sourcedoc.Section
	anchors  []sourcedoc.Anchor
}

func sectionMarksJSON(t *testing.T, marks map[string]int) string {
	t.Helper()
	type mark struct {
		Rune    int    `json:"rune"`
		Heading string `json:"heading"`
	}
	list := []mark{}
	for _, heading := range []string{"Abstract", "Methods", "Experiments", "Limitations", "References"} {
		if at, ok := marks[heading]; ok {
			list = append(list, mark{at, heading})
		}
	}
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func articleFixture(t *testing.T) sectionedInput {
	t.Helper()
	parts := []string{
		"Abstract\nМембранный электролизёр описан как способ получения водорода.\n",
		"Methods\nЭлектролизёр собран на протонообменной мембране нового состава.\n",
		"Experiments\nУстановка отработала тысячу часов при постоянной плотности тока.\n",
		"Limitations\nРесурс мембраны за пределами тысячи часов не измерялся.\n",
		"References\nОтчёт лаборатории за прошлый год.\n",
	}
	marks := map[string]int{}
	at := 0
	for _, part := range parts {
		heading, _, _ := strings.Cut(part, "\n")
		marks[heading] = at
		at += utf8.RuneCountInString(part)
	}
	text := strings.Join(parts, "")
	return sectionedInput{name: "pdf article", source: "pdf", text: text,
		metadata: map[string]string{"section_offsets": sectionMarksJSON(t, marks)}}
}

func patentFixture() sectionedInput {
	abstract := "Устройство отображения содержит подложку из перовскита."
	claims := "1. Устройство отображения, содержащее подложку. 2. Устройство по пункту 1 с маской."
	text := abstract + "\n\n" + claims
	abstractEnd := utf8.RuneCountInString(abstract)
	return sectionedInput{name: "patent xml", source: "patent", text: text,
		sections: []sourcedoc.Section{
			{Name: sourcedoc.SectionAbstract, Start: 0, End: abstractEnd},
			{Name: sourcedoc.SectionClaims, Start: abstractEnd + 2, End: utf8.RuneCountInString(text)},
		},
		anchors: []sourcedoc.Anchor{{Path: "claim/1", Start: abstractEnd + 2, End: utf8.RuneCountInString(text)}}}
}

func webFixture() sectionedInput {
	first := "Перовскитный модуль сохранил эффективность после термоциклирования."
	second := "Натриевый аккумулятор выдержал полное циклирование в независимом испытании."
	text := first + "\n" + second
	return sectionedInput{name: "web html", source: "web", text: text,
		anchors: []sourcedoc.Anchor{
			{Path: "html/body/article/p[1]", Start: 0, End: utf8.RuneCountInString(first)},
			{Path: "html/body/article/p[2]", Start: utf8.RuneCountInString(first) + 1, End: utf8.RuneCountInString(text)},
		}}
}

func scanFixture() sectionedInput {
	return sectionedInput{name: "ocr scan", source: "ocr",
		text:     "Сканированный отчёт о натриевом аккумуляторе, распознанный без единого заголовка.\n",
		metadata: map[string]string{"page_offsets": "[0]"}}
}

func TestStoredTextReproducesEveryLocator(t *testing.T) {
	for _, input := range []sectionedInput{articleFixture(t), patentFixture(), webFixture(), scanFixture()} {
		t.Run(input.name, func(t *testing.T) {
			h := newIndexHarness(t)
			h.control.source.Sections = input.sections
			h.control.source.Anchors = input.anchors
			evt := &commonv1.DocumentParsed{
				DocumentId: h.control.document.GetId(), OwnerId: "alice", ObjectKey: "raw/pdf",
				Source: input.source, Filename: "source." + input.source, MimeType: "text/plain", Text: input.text,
				Metadata: contracts.ParsedMetadata(input.text, "parser-v1", input.metadata),
			}
			if err := h.ix.Process(t.Context(), evt); err != nil {
				t.Fatal(err)
			}
			var snapshot indexdoc.Snapshot
			if err := json.Unmarshal(h.objects.objects[h.control.active.Data.Key], &snapshot); err != nil {
				t.Fatal(err)
			}
			runes := []rune(input.text)
			if indexdoc.Hash([]byte(input.text)) != snapshot.TextHash {
				t.Fatal("the stored text is not the text the locators address")
			}
			for _, chunk := range snapshot.Chunks {
				locator := indexdoc.Locator{Section: chunk.Section, StartChar: chunk.CharStart, EndChar: chunk.CharEnd}
				if err := locator.Verify(runes, chunk.Text); err != nil {
					t.Fatalf("chunk %d does not reproduce from its locator: %v", chunk.Index, err)
				}
			}
			if err := sourcedoc.ValidateSections(snapshot.Sections, len(runes)); err != nil {
				t.Fatalf("sections do not address the stored text: %v", err)
			}
			if err := sourcedoc.ValidateAnchors(snapshot.Anchors, len(runes)); err != nil {
				t.Fatalf("anchors do not address the stored text: %v", err)
			}
			for _, section := range snapshot.Sections {
				if !strings.Contains(input.text, string(runes[section.Start:section.End])) {
					t.Fatalf("section %s does not reproduce from its locator", section.Name)
				}
			}
			for _, anchor := range snapshot.Anchors {
				if !strings.Contains(input.text, string(runes[anchor.Start:anchor.End])) {
					t.Fatalf("anchor %s does not reproduce from its locator", anchor.Path)
				}
			}
			checkExpectedStructure(t, input, snapshot)
		})
	}
}

// Для каждого типа входа требуется ожидаемая структура, чтобы пустая не проходила все правила тривиально.
func checkExpectedStructure(t *testing.T, input sectionedInput, snapshot indexdoc.Snapshot) {
	t.Helper()
	names := make([]string, 0, len(snapshot.Sections))
	for _, section := range snapshot.Sections {
		names = append(names, section.Name)
	}
	switch input.name {
	case "pdf article":
		want := "abstract,method,experiments,limitations,references"
		if strings.Join(names, ",") != want {
			t.Fatalf("the article sections are %v, want %s", names, want)
		}
		if snapshot.Sections[0].Title != "Abstract" {
			t.Fatal("the heading the document used was thrown away")
		}
	case "patent xml":
		if strings.Join(names, ",") != "abstract,claims" || len(snapshot.Anchors) != 1 ||
			snapshot.Anchors[0].Path != "claim/1" {
			t.Fatalf("the collector structure did not reach the snapshot: %v %+v", names, snapshot.Anchors)
		}
	case "web html":
		if len(snapshot.Anchors) != 2 || snapshot.Anchors[1].Path != "html/body/article/p[2]" {
			t.Fatalf("the element locators did not reach the snapshot: %+v", snapshot.Anchors)
		}
		if snapshot.Chunks[0].LocatorKind != "html_element" ||
			snapshot.Chunks[0].Metadata["element_path"] != "html/body/article/p[1]" {
			t.Fatalf("a web chunk does not name the element it came from: %+v", snapshot.Chunks[0].Metadata)
		}
	case "ocr scan":
		// Скан без распознанного заголовка не несёт разделов вовсе, а не называет неадресуемый.
		if len(snapshot.Sections) != 0 || len(snapshot.Anchors) != 0 {
			t.Fatalf("a scan without structure declared one: %v %+v", names, snapshot.Anchors)
		}
	}
}

func TestPlainTextByReferenceStillPinsItsRevision(t *testing.T) {
	text := "Мембранный электролизёр описан как способ получения водорода из воды.\n"
	for name, tweak := range map[string]func(*commonv1.DocumentParsed){
		"by reference": func(evt *commonv1.DocumentParsed) {
			evt.Text, evt.TextObjectKey = "", "text/plain-source"
			evt.Metadata = map[string]string{"parser_revision": "office-extractor-v1"}
		},
		"declared hash": func(*commonv1.DocumentParsed) {},
	} {
		t.Run(name, func(t *testing.T) {
			h := newIndexHarness(t)
			h.objects.objects["text/plain-source"] = []byte(text)
			evt := &commonv1.DocumentParsed{
				DocumentId: h.control.document.GetId(), OwnerId: "alice", ObjectKey: "raw/pdf",
				Source: "office", Filename: "source.txt", MimeType: "text/plain", Text: text,
				Metadata: contracts.ParsedMetadata(text, "office-extractor-v1", nil),
			}
			tweak(evt)
			if err := h.ix.Process(t.Context(), evt); err != nil {
				t.Fatalf("a plain-text source did not become a revision: %v", err)
			}
			if h.control.active == nil || h.control.active.ChunkCount == 0 {
				t.Fatal("no snapshot was published")
			}
		})
	}

	h := newIndexHarness(t)
	evt := &commonv1.DocumentParsed{
		DocumentId: h.control.document.GetId(), OwnerId: "alice", ObjectKey: "raw/pdf",
		Source: "office", Filename: "source.txt", MimeType: "text/plain", Text: text,
		Metadata: contracts.ParsedMetadata(text+" changed", "office-extractor-v1", nil),
	}
	if err := h.ix.Process(t.Context(), evt); err == nil {
		t.Fatal("a text that disagrees with its declared hash became a revision")
	}
}
