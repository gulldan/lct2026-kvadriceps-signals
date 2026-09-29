package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestEPRecordFromRealDocument(t *testing.T) {
	raw, err := os.ReadFile("testdata/ep-1004359-b1.xml")
	if err != nil {
		t.Fatal(err)
	}
	row, err := epRecord(raw, "EP1004359NWB1", epBase+"/patents/EP1004359NWB1/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != "EP1004359B1" || row.KindCode != "B1" || row.Date != "2013-09-04" {
		t.Fatalf("identity or publication date misread: %+v", row)
	}
	if row.Title == "" || row.Application != "99123086.3" || row.Filing != "1999-11-22" {
		t.Fatalf("title, application or filing date misread: %q %q %q", row.Title, row.Application, row.Filing)
	}
	if row.Priority != "1998-11-25" || row.PriorityBasis == "" ||
		!strings.HasPrefix(row.FamilyID, "priority:") {
		t.Fatalf("priority or family fallback misread: %q %q %q", row.Priority, row.PriorityBasis, row.FamilyID)
	}
	if row.TextStatus != patentFullText || row.FullText == nil {
		t.Fatalf("a granted EP publication produced no full text: %+v", row.TextStatus)
	}

	runes := []rune(row.Text)
	names := make([]string, 0, len(row.FullText.Sections))
	for _, section := range row.FullText.Sections {
		names = append(names, section.Name)
		if section.End > len(runes) || section.End <= section.Start {
			t.Fatalf("section %s is not addressable: %+v", section.Name, section)
		}
		if !strings.Contains(row.Text, string(runes[section.Start:section.End])) {
			t.Fatalf("section %s does not reproduce from its locator", section.Name)
		}
	}
	if strings.Join(names, ",") != "description,claims" {
		t.Fatalf("unexpected sections: %v", names)
	}
	if !slices.Contains(row.FullText.Missing, "abstract") {
		t.Fatalf("a missing section was not reported: %+v", row.FullText.Missing)
	}
	if len(row.FullText.Claims) != 21 {
		t.Fatalf("expected 21 claims, got %d", len(row.FullText.Claims))
	}
	for _, claim := range row.FullText.Claims {
		if claim.End > len(runes) || claim.End <= claim.Start {
			t.Fatalf("claim %d is not addressable: %+v", claim.Number, claim)
		}
		if !strings.Contains(row.Text, string(runes[claim.Start:claim.End])) {
			t.Fatalf("claim %d does not reproduce from its locator", claim.Number)
		}
	}
	if row.FullText.Claims[0].Dependent {
		t.Fatal("the first claim was read as depending on an earlier one")
	}
	if !slices.ContainsFunc(row.FullText.Claims, func(c patentClaim) bool { return c.Dependent }) {
		t.Fatal("no dependent claim was recognised in a 21-claim patent")
	}
	if !strings.Contains(strings.Join(row.Limitations, " | "), "family_unavailable") {
		t.Fatalf("the missing family was not reported: %+v", row.Limitations)
	}
	if row.Language != "en" || row.ParserVersion != epParserVersion {
		t.Fatalf("language or parser version lost: %q %q", row.Language, row.ParserVersion)
	}
}

func TestEPWeekListKeepsOnlyPublicationsWithText(t *testing.T) {
	list := strings.Join([]string{
		`<a href="/publication-server/rest/v1.2/patents/EP4801253NWA1">EP4801253NWA1</a>`,
		`<a href="/publication-server/rest/v1.2/patents/EP4801254NWA2">EP4801254NWA2</a>`,
		`<a href="/publication-server/rest/v1.2/patents/EP4623763NWA3">EP4623763NWA3</a>`,
		`<a href="/publication-server/rest/v1.2/patents/EP2151037NWB1">EP2151037NWB1</a>`,
		`<a href="/publication-server/rest/v1.2/patents/EP2665753NWB2">EP2665753NWB2</a>`,
		`<a href="/publication-server/rest/v1.2/patents/EP2151037NWB1">EP2151037NWB1</a>`,
	}, "\n")
	got := epWeekPatents([]byte(list))
	if !slices.Equal(got, []string{"EP2151037NWB1", "EP2665753NWB2"}) {
		t.Fatalf("the week list was not narrowed to publications carrying text: %v", got)
	}
	if id := epRecordID("EP2151037NWB1"); id != "EP2151037B1" {
		t.Fatalf("the record identity of a list entry is %q", id)
	}
}

func TestEPPublicationDatesStayInsideTheWindow(t *testing.T) {
	body := []byte(strings.Join([]string{
		`href="/publication-server/rest/v1.2/publication-dates/20260909/patents"`,
		`href="/publication-server/rest/v1.2/publication-dates/20260902/patents"`,
		`href="/publication-server/rest/v1.2/publication-dates/20240103/patents"`,
		`href="/publication-server/rest/v1.2/publication-dates/19790110/patents"`,
	}, "\n"))
	got := epPublicationDates(body, "2026-08-01", "2026-09-13")
	if !slices.Equal(got, []string{"20260909", "20260902"}) {
		t.Fatalf("the sweep left its window: %v", got)
	}
	if len(epPublicationDates(body, "2030-01-01", "2030-12-31")) != 0 {
		t.Fatal("a window with no publication week produced weeks")
	}
}
