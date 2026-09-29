package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const cnSource = "Google Patents · CN publications"
const cnBase = "https://patents.google.com"
const cnParserVersion = "google-patents-cn-html-v2"
const cnAbstract = "abstract"
const cnDescription = "description"

var cnID = regexp.MustCompile(`^CN[0-9]{7,12}([ABCUS][0-9]?)$`)

// cnRecord хранит китайский текст как свидетельство, перевод — отдельная операция; даты публикации и выдачи различны.
func cnRecord(raw []byte, id string) (record, error) {
	parts := cnID.FindStringSubmatch(id)
	if len(parts) != 2 {
		return record{}, errors.New("invalid CN publication number")
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return record{}, fmt.Errorf("parse CN publication: %w", err)
	}
	row := record{ID: id, KindCode: parts[1], URL: cnBase + "/patent/" + id + "/zh",
		Source: cnSource, ParserVersion: cnParserVersion, TextStatus: metadataOnlyText,
		DateBasis: "Google Patents publicationDate", TitleBasis: "Google Patents DC.title"}
	facts := &patentFullTextFacts{GUID: id}
	publication, metaDate := cnBibliography(doc, &row, facts)
	if publication != id || row.Date == "" || row.Title == "" {
		return record{}, errors.New("CN publication identity, title or publication date missing or mismatched")
	}
	// Даты метаданных обязаны совпасть: дата родственной публикации не подменяет отсутствующую.
	if metaDate != row.Date {
		return record{}, errors.New("CN publication date does not agree with DC.date")
	}
	if row.Application != "" {
		row.FamilyID, row.FamilyBasis = "application:"+row.Application, "CN application number; not a worldwide family"
	}
	row.Limitations = append(row.Limitations, "family_unavailable; only the same CN application is grouped",
		"priority_unverified; displayed prior-art date is not treated as a verified priority claim")
	if err := cnSections(cnTextSections(doc), &row, facts); err != nil {
		return record{}, err
	}
	row.CanonicalURL = row.URL
	row.Provenance = []provenance{{URL: row.URL, SHA256: hashBytes(raw)}}
	return row, nil
}

func cnBibliography(doc *html.Node, row *record, facts *patentFullTextFacts) (publication, metaDate string) {
	// У выданных публикаций Google пишет номер и дату в полях выдачи; заявка A обязана дать дату публикации.
	numberField, dateScheme := "citation_patent_publication_number", ""
	switch strings.TrimRight(row.KindCode, "0123456789") {
	case "B", "C", "U", "S":
		numberField, dateScheme = "citation_patent_number", "issue"
	}
	visit(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		cnBibliographicField(n, row)
		if n.Data == "meta" {
			switch attr(n, "name") {
			case numberField:
				publication = strings.ReplaceAll(attr(n, "content"), ":", "")
			case "DC.title":
				row.Title = strings.Join(strings.Fields(attr(n, "content")), " ")
			case "DC.date":
				if attr(n, "scheme") == dateScheme {
					metaDate = sourceDay(attr(n, "content"))
				}
			case "DC.contributor":
				if attr(n, "scheme") == "assignee" {
					facts.Assignees = append(facts.Assignees, attr(n, "content"))
				}
			}
		}
	})
	return publication, metaDate
}

// Первая дата — из библиографии: таблицы событий, семейства и похожих документов идут после неё.
func cnBibliographicField(n *html.Node, row *record) {
	switch n.Data + ":" + attr(n, "itemprop") {
	case "time:publicationDate":
		if row.Date == "" {
			row.Date = sourceDay(attr(n, "datetime"))
		}
	case "dd:applicationNumber":
		if row.Application == "" {
			row.Application = strings.TrimSpace(content(n))
		}
	case "time:filingDate":
		if row.Filing == "" {
			row.Filing = sourceDay(attr(n, "datetime"))
			row.FilingBasis = "Google Patents filingDate"
		}
	}
}

func cnTextSections(doc *html.Node) map[string]*html.Node {
	sections := map[string]*html.Node{}
	visit(doc, func(n *html.Node) {
		if prop := attr(n, "itemprop"); n.Data == "section" &&
			slices.Contains([]string{cnAbstract, cnDescription, sectionClaims}, prop) {
			sections[prop] = cnProperty(n, "content")
		}
	})
	return sections
}

func cnProperty(root *html.Node, name string) *html.Node {
	var found *html.Node
	visit(root, func(n *html.Node) {
		if found == nil && attr(n, "itemprop") == name {
			found = n
		}
	})
	return found
}

// Пункты формулы собираются один раз, включая вложенные claim-text; смещения — в кодовых точках.
func cnSections(nodes map[string]*html.Node, row *record, facts *patentFullTextFacts) error {
	var body strings.Builder
	at := 0
	for _, name := range []string{cnAbstract, cnDescription, sectionClaims} {
		n := nodes[name]
		if n == nil {
			facts.Missing = append(facts.Missing, name)
			continue
		}
		cnImageLimitations(n, row)
		text, _ := textNodeAnchored(n)
		var claims []patentClaim
		if name == sectionClaims {
			text, claims = cnClaimsText(n)
		}
		if text == "" {
			facts.Missing = append(facts.Missing, name)
			continue
		}
		if language := cnSectionLanguage(n); language != "zh" {
			return fmt.Errorf("CN %s language is %q; original Chinese text could not be verified", name, language)
		}
		row.Language = "zh"
		if at > 0 {
			body.WriteByte('\n')
			at++
		}
		start := at
		body.WriteString(text)
		at += utf8.RuneCountInString(text)
		facts.Sections = append(facts.Sections, patentSection{Name: name, Start: start, End: at})
		for _, claim := range claims {
			claim.Start, claim.End = claim.Start+start, claim.End+start
			facts.Claims = append(facts.Claims, claim)
		}
	}
	row.Text, row.FullText = body.String(), facts
	if row.Text == "" {
		row.Limitations = append(row.Limitations, "full_text_unavailable; publication contains no readable sections")
		return nil
	}
	row.ContentHash, row.TextStatus = hashBytes([]byte(row.Text)), patentFullText
	if len(facts.Sections) == 1 && facts.Sections[0].Name == cnAbstract {
		row.TextStatus = "abstract_only"
	}
	if len(facts.Missing) > 0 {
		row.Limitations = append(row.Limitations, "patent_sections_partial; missing "+strings.Join(facts.Missing, ", "))
	}
	return nil
}

func cnClaimsText(n *html.Node) (string, []patentClaim) {
	var body strings.Builder
	claims := []patentClaim{}
	at := 0
	visit(n, func(claim *html.Node) {
		num, err := strconv.Atoi(attr(claim, "num"))
		if err != nil || num <= 0 || !slices.Contains(strings.Fields(attr(claim, "class")), "claim") {
			return
		}
		text, _ := textNodeAnchored(claim)
		if text == "" {
			return
		}
		if at > 0 {
			body.WriteByte('\n')
			at++
		}
		start := at
		body.WriteString(text)
		at += utf8.RuneCountInString(text)
		claims = append(claims, patentClaim{Number: num, Start: start, End: at,
			Dependent: strings.Contains(text, "权利要求") && num > 1})
	})
	return body.String(), claims
}

// Окружающий интерфейс Google на странице /zh объявляет lang=en: язык патента — только у самого раздела.
func cnSectionLanguage(n *html.Node) string {
	code := ""
	visit(n, func(el *html.Node) {
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(attr(el, "lang")))
		}
	})
	code, _, _ = strings.Cut(code, "-")
	code, _, _ = strings.Cut(code, "_")
	return code
}

func cnImageLimitations(n *html.Node, row *record) {
	visit(n, func(el *html.Node) {
		if el.Data == "img" || strings.Contains(attr(el, "class"), "patent-image-not-available") {
			row.Limitations = appendLimitation(row.Limitations,
				"patent_images_not_transcribed; figures and image-only equations require the original document")
		}
	})
}
