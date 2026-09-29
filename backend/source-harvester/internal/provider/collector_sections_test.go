package provider

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

func TestCollectorSectionsAddressTheStoredText(t *testing.T) {
	body := "Устройство отображения содержит подложку.\n\nФормула. Устройство по пункту 1."
	abstract := "Устройство отображения содержит подложку."
	row := collectorRecord{Title: "Устройство отображения", Text: body}
	row.FullText = &collectorFullText{Sections: []sourcedoc.Section{
		{Name: "abstract", Start: 0, End: utf8.RuneCountInString(abstract)},
		{Name: "claims", Start: utf8.RuneCountInString(abstract) + 2, End: utf8.RuneCountInString(body)},
	}}

	text := strings.TrimSpace(row.Title + "\n\n" + row.Text)
	sections := collectorSections(row, text)
	if len(sections) != 2 {
		t.Fatalf("the collector structure was lost at the boundary: %+v", sections)
	}
	runes := []rune(text)
	if got := string(runes[sections[0].Start:sections[0].End]); got != abstract {
		t.Fatalf("the abstract locator reproduces %q, want %q", got, abstract)
	}
	if got := string(runes[sections[1].Start:sections[1].End]); !strings.HasPrefix(got, "Формула.") {
		t.Fatalf("the claims locator reproduces %q", got)
	}
	if err := sourcedoc.ValidateSections(sections, len(runes)); err != nil {
		t.Fatalf("the shifted structure does not address the stored text: %v", err)
	}

	beyond := row
	beyond.Text = "короче"
	if got := collectorSections(beyond, strings.TrimSpace(beyond.Title+"\n\n"+beyond.Text)); got != nil {
		t.Fatalf("an unaddressable structure was kept: %+v", got)
	}
	if collectorSections(collectorRecord{Title: "t", Text: body}, text) != nil {
		t.Fatal("a record without collector structure produced sections")
	}

	row.FullText.Claims = []collectorClaim{
		{Number: 1, Start: utf8.RuneCountInString(abstract) + 10, End: utf8.RuneCountInString(body)},
	}
	anchors := collectorAnchors(row, text)
	if len(anchors) != 1 || anchors[0].Path != "claim/1" {
		t.Fatalf("the patent claim locator was lost at the boundary: %+v", anchors)
	}
	if got := string(runes[anchors[0].Start:anchors[0].End]); strings.TrimSpace(got) != "Устройство по пункту 1." {
		t.Fatalf("the claim locator reproduces %q", got)
	}
	web := collectorRecord{Title: "Заголовок", Text: body,
		Anchors: []sourcedoc.Anchor{{Path: "html/body/article/p[2]", Start: 0, End: utf8.RuneCountInString(abstract)}}}
	webText := strings.TrimSpace(web.Title + "\n\n" + web.Text)
	webAnchors := collectorAnchors(web, webText)
	if len(webAnchors) != 1 ||
		string([]rune(webText)[webAnchors[0].Start:webAnchors[0].End]) != abstract {
		t.Fatalf("the web element locator does not reproduce its block: %+v", webAnchors)
	}
}
