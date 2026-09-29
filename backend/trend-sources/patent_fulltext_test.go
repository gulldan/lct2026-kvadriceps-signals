package main

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"
)

func highlightFixture() patentHighlight {
	abstract := "<p>A display device includes a substrate.</p>"
	brief := "<p>(1) This application claims priority to a Korean application.</p>"
	description := "<p>BRIEF DESCRIPTION OF THE DRAWINGS<br />(1) The accompanying drawings.</p>"
	claims := "<b>1</b>. A display device comprising a substrate. " +
		"<b>2</b>. The display device of claim 1, further comprising a mask. " +
		"<b>3</b>. A method of manufacturing a display device. " +
		"<b>4</b>. The method according to claim 3, wherein the mask is fine."
	return patentHighlight{
		GUID: "US-12733373-B2", CompositeID: "1000009523490!US-US-12733373", PageCount: 47, Language: "EN",
		Abstract: &abstract, Brief: &brief, Description: &description, Claims: &claims,
		Assignees: []string{"SAMSUNG DISPLAY CO., LTD."}, Applicants: []string{"Samsung Display Co., LTD."},
		CPC: "H10K59/8723;H10K59/1201", USReferences: []string{"US 11145700 B2 Choi et al."},
		Kind: []string{"B2"}, Type: patentSearchSource, Title: "Display device",
	}
}

func TestPatentFullTextSections(t *testing.T) {
	page := highlightFixture()
	text, sections, claims, missing := patentSections(page)
	runes := []rune(text)
	names := make([]string, 0, len(sections))
	for _, section := range sections {
		names = append(names, section.Name)
		if section.Start < 0 || section.End <= section.Start || section.End > len(runes) {
			t.Fatalf("section %s is not addressable in the assembled text: %+v", section.Name, section)
		}
	}
	if strings.Join(names, ",") != "abstract,summary,description,claims" {
		t.Fatalf("unexpected section order: %v", names)
	}
	if len(missing) != 1 || missing[0] != "background" {
		t.Fatalf("a section the patent does not have must be reported: %v", missing)
	}
	if len(claims) != 4 {
		t.Fatalf("expected four claims, got %d", len(claims))
	}
	for _, claim := range claims {
		if claim.End > len(runes) || !strings.Contains(text, string(runes[claim.Start:claim.End])) {
			t.Fatalf("claim %d does not reproduce from its locator", claim.Number)
		}
	}
	if claims[0].Dependent || !claims[1].Dependent || claims[2].Dependent || !claims[3].Dependent {
		t.Fatalf("claim dependency misread: %+v", claims)
	}

	row, err := applyPatentFullText(record{ID: "US12733373", KindCode: "B2", ParserVersion: patentMetadataVersion},
		page, provenance{URL: "https://ppubs.uspto.gov/api/patents/highlight/US-12733373-B2", SHA256: hashBytes([]byte("x"))},
		"2026-09-12T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.TextStatus != patentFullText || row.ContentHash != hashBytes([]byte(row.Text)) ||
		!parserIncludes(row.ParserVersion, patentFullTextVersion) ||
		!parserIncludes(row.ParserVersion, patentMetadataVersion) {
		t.Fatalf("full text did not replace the representative claim: %+v", row)
	}
	if row.FullText == nil || row.FullText.PageCount != 47 || len(row.FullText.CPC) != 2 ||
		row.FullText.Assignees[0] != "SAMSUNG DISPLAY CO., LTD." || row.Language != "EN" {
		t.Fatalf("participants, classification or language lost: %+v", row.FullText)
	}
	for _, claim := range row.FullText.Claims {
		if claim.End > len([]rune(row.Text)) || claim.End <= claim.Start {
			t.Fatalf("claim %d lost its locator after assembly", claim.Number)
		}
	}
}

// Нужен не-ASCII текст: только он отличает кодовые точки от байтов.
func TestPatentLocatorsAreCodePoints(t *testing.T) {
	page := highlightFixture()
	abstract := "<p>Устройство отображения содержит подложку из перовскита.</p>"
	claims := "<b>1</b>. Устройство отображения, содержащее подложку. " +
		"<b>2</b>. Устройство по пункту 1, дополнительно содержащее маску."
	page.Abstract, page.Claims = &abstract, &claims
	text, sections, patentClaimList, _ := patentSections(page)
	runes := []rune(text)
	if len(runes) == len(text) {
		t.Fatal("the fixture must contain multi-byte text, otherwise it proves nothing about the unit")
	}
	for _, section := range sections {
		quote := string(runes[section.Start:section.End])
		if !strings.Contains(text, quote) || quote == "" {
			t.Fatalf("section %s does not reproduce from its code-point locator", section.Name)
		}
	}
	if sections[0].Name != "abstract" ||
		string(runes[sections[0].Start:sections[0].End]) != "Устройство отображения содержит подложку из перовскита." {
		t.Fatalf("the abstract locator does not reproduce the abstract: %q",
			string(runes[sections[0].Start:sections[0].End]))
	}
	for _, claim := range patentClaimList {
		if !strings.Contains(text, string(runes[claim.Start:claim.End])) {
			t.Fatalf("claim %d does not reproduce from its code-point locator", claim.Number)
		}
	}
	if len(patentClaimList) != 2 {
		t.Fatalf("expected two claims in the non-ASCII document, got %+v", patentClaimList)
	}
}

func TestPatentFullTextRejectsForeignDocument(t *testing.T) {
	page := highlightFixture()
	page.Claims = nil
	if _, err := applyPatentFullText(record{ID: "US12733373"}, page, provenance{}, "2026-09-12T00:00:00Z"); err == nil {
		t.Fatal("a document without claims was accepted as full text")
	}
	if guid := patentGUID("US12733373", "B2"); guid != "US-12733373-B2" {
		t.Fatalf("unexpected document identifier %q", guid)
	}
	if patentGUID("US12733373", "") != "" || patentGUID("not-a-patent", "B2") != "" {
		t.Fatal("an unverified identity produced a document request")
	}
}

// Нечитаемый полный текст одного патента — оговорка о нём: следующие патенты получают свой текст.
func TestPatentFullTextUnreadableDocumentDoesNotStopLaterOnes(t *testing.T) {
	t.Parallel()
	valid, err := json.Marshal(highlightFixture())
	if err != nil {
		t.Fatal(err)
	}
	s := &service{kind: kindPatents, dir: t.TempDir(), config: defaultConfig()}
	s.config.PatentTerms = nil
	s.http = &http.Client{Transport: ledgerTransport(func(req *http.Request) (*http.Response, error) {
		body := string(valid)
		if strings.Contains(req.URL.Path, "US-12733372-B2") {
			body = "<html>maintenance</html>"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := newPatentSearchClient(s)
	client.interval, client.token = 0, "fixture"
	data := &snapshot{ObservedAt: "2026-09-12T00:00:00Z", Records: []record{
		{ID: "US12733372", KindCode: "B2", ParserVersion: patentMetadataVersion, Source: patentSource},
		{ID: "US12733373", KindCode: "B2", ParserVersion: patentMetadataVersion, Source: patentSource},
	}}
	if err = s.patentFullTexts(t.Context(), client, data, &patentMetadataProgress{}); err != nil ||
		data.Records[1].TextStatus != patentFullText || len(data.Records[0].Limitations) != 1 {
		t.Fatalf("one unreadable full text stopped the rest: err=%v records=%+v", err, data.Records)
	}
}
