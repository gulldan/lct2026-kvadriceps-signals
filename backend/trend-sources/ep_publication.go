package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// European Publication Server не требует ключа, предел — 10 ГБ на IP за скользящую неделю. Поле, которого
// нет у EPO, остаётся пустым и называется, а не берётся из записи USPTO родственного документа.
const epBase = "https://data.epo.org/publication-server/rest/v1.2"
const epParserVersion = "epo-publication-server-v1.2"
const epSource = "EPO European Publication Server"

// epGranted — коды с формулой и описанием: у A1/A2 только библиография, A3 добавляет лишь реферат.
func epGranted() []string { return []string{"B1", "B2", "B8", "B9"} }

var epPatentID = regexp.MustCompile(`/patents/(EP[0-9]+[A-Z]{2}([AB][0-9]))`)
var epPublicationDate = regexp.MustCompile(`/publication-dates/([0-9]{8})/patents`)

type epDocument struct {
	XMLName       xml.Name `xml:"ep-patent-document"`
	DocNumber     string   `xml:"doc-number,attr"`
	Kind          string   `xml:"kind,attr"`
	DatePubl      string   `xml:"date-publ,attr"`
	Lang          string   `xml:"lang,attr"`
	Country       string   `xml:"country,attr"`
	Bibliographic struct {
		PublicationNumber string `xml:"B100>B110"`
		PublicationDate   string `xml:"B100>B140>date"`
		Application       string `xml:"B200>B210"`
		FilingDate        string `xml:"B200>B220>date"`
		Priorities        []struct {
			Number  string `xml:"B310"`
			Date    string `xml:"B320>date"`
			Country string `xml:"B330>ctry"`
		} `xml:"B300"`
		Titles []struct {
			Language string `xml:"B541"`
			Value    string `xml:"B542"`
		} `xml:"B500>B540"`
		Applicants []struct {
			Name string `xml:"snm"`
		} `xml:"B700>B730>B731"`
		Inventors []struct {
			Name string `xml:"snm"`
		} `xml:"B700>B720>B721"`
	} `xml:"SDOBI"`
	Abstract    epText    `xml:"abstract"`
	Description epText    `xml:"description"`
	Claims      []epClaim `xml:"claims>claim"`
}

type epText struct {
	Inner string `xml:",innerxml"`
}

type epClaim struct {
	Num   string `xml:"num,attr"`
	Inner string `xml:",innerxml"`
}

func epPublicationDates(body []byte, from, through string) []string {
	dates := []string{}
	for _, match := range epPublicationDate.FindAllStringSubmatch(string(body), -1) {
		day := match[1][:4] + "-" + match[1][4:6] + "-" + match[1][6:]
		if day >= from && day <= through {
			dates = append(dates, match[1])
		}
	}
	slices.Sort(dates)
	slices.Reverse(dates)
	return dates
}

// epWeekPatents: список недели отдаёт только номера, и код вида — единственный фильтр до чтения документа.
func epWeekPatents(body []byte) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, match := range epPatentID.FindAllStringSubmatch(string(body), -1) {
		if seen[match[1]] || !slices.Contains(epGranted(), match[2]) {
			continue
		}
		seen[match[1]] = true
		ids = append(ids, match[1])
	}
	return ids
}

func epRecord(raw []byte, id, url string) (record, error) {
	var doc epDocument
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return record{}, fmt.Errorf("parse EP document %s: %w", id, err)
	}
	number := strings.TrimSpace(doc.Bibliographic.PublicationNumber)
	if number == "" || doc.Kind == "" {
		return record{}, errors.New("EP document carries no publication number or kind")
	}
	published := epDay(doc.Bibliographic.PublicationDate, doc.DatePubl)
	if published == "" {
		return record{}, errors.New("EP document carries no publication date")
	}
	row := record{
		ID: "EP" + number + doc.Kind, KindCode: doc.Kind, Date: published,
		DateBasis: "EPO publication date (ST.32 B140)", TitleBasis: "EPO title (ST.32 B542)",
		URL: url, Source: epSource, TextStatus: metadataOnlyText, ParserVersion: epParserVersion,
		Language: strings.ToLower(doc.Lang),
	}
	row.Title = epTitle(doc)
	if row.Title == "" {
		return record{}, errors.New("EP document carries no title")
	}
	row.Application = strings.TrimSpace(doc.Bibliographic.Application)
	if filing := epDay(doc.Bibliographic.FilingDate, ""); filing != "" {
		row.Filing, row.FilingBasis = filing, "EPO filing date (ST.32 B220)"
	}
	epPriorities(doc, &row)
	text, sections, claims, missing := epSections(doc)
	if text != "" {
		row.Text, row.TextStatus = text, patentFullText
		row.ContentHash = hashBytes([]byte(text))
		row.FullText = &patentFullTextFacts{
			GUID: row.ID, Sections: sections, Claims: claims, Missing: missing,
			Applicants: epNames(doc, true), Assignees: epNames(doc, true),
		}
		if len(missing) > 0 {
			row.Limitations = appendLimitation(row.Limitations,
				"patent_sections_partial; EPO returned no "+strings.Join(missing, ", "))
		}
	} else {
		row.Limitations = appendLimitation(row.Limitations,
			"full_text_unavailable; this EP publication carries bibliographic data only")
	}
	// Семейство EP даёт DOCDB или INPADOC, а им нужен аккаунт OPS. Без него честная группировка — только
	// приоритетная заявка: та же подача, а не мировое семейство.
	row.Limitations = appendLimitation(row.Limitations,
		"family_unavailable; EP families need EPO OPS, which is not configured, so only the priority application groups")
	return row, nil
}

func epTitle(doc epDocument) string {
	titles := doc.Bibliographic.Titles
	for _, want := range []string{"en", "de", "fr"} {
		for _, title := range titles {
			if strings.EqualFold(strings.TrimSpace(title.Language), want) {
				if value := strings.Join(strings.Fields(title.Value), " "); value != "" {
					return value
				}
			}
		}
	}
	for _, title := range titles {
		if value := strings.Join(strings.Fields(title.Value), " "); value != "" {
			return value
		}
	}
	return ""
}

func epNames(doc epDocument, applicants bool) []string {
	out := []string{}
	if applicants {
		for _, applicant := range doc.Bibliographic.Applicants {
			if name := strings.Join(strings.Fields(applicant.Name), " "); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// epPriorities: дата подачи — не дата приоритета, отсутствующий приоритет остаётся пустым.
func epPriorities(doc epDocument, row *record) {
	for _, priority := range doc.Bibliographic.Priorities {
		day := epDay(priority.Date, "")
		number := strings.Join(strings.Fields(priority.Number), "")
		if day == "" || number == "" {
			continue
		}
		claim := strings.TrimSpace(priority.Country) + number
		row.PriorityClaims = append(row.PriorityClaims, claim)
		if row.Priority == "" || day < row.Priority {
			row.Priority, row.PriorityBasis = day, "EPO priority claim (ST.32 B300)"
		}
	}
	switch {
	case len(row.PriorityClaims) > 0:
		row.FamilyID = "priority:" + slices.Min(row.PriorityClaims)
		row.FamilyBasis = "earliest claimed priority application"
	case row.Application != "":
		row.FamilyID = "application:" + row.Application
		row.FamilyBasis = "EP application number"
	}
}

// epSections называет раздел, не отданный EPO, отсутствующим, а не хранит пустым.
func epSections(doc epDocument) (string, []patentSection, []patentClaim, []string) {
	ordered := []struct{ name, text string }{
		{sectionAbstract, textHTML(doc.Abstract.Inner)},
		{"description", textHTML(doc.Description.Inner)},
		{sectionClaims, epClaimsText(doc.Claims)},
	}
	var body strings.Builder
	sections := []patentSection{}
	missing := []string{}
	runes := 0
	claimsStart := -1
	for _, section := range ordered {
		if section.text == "" {
			missing = append(missing, section.name)
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
			runes += 2
		}
		start := runes
		body.WriteString(section.text)
		runes += utf8.RuneCountInString(section.text)
		sections = append(sections, patentSection{Name: section.name, Start: start, End: runes})
		if section.name == sectionClaims {
			claimsStart = start
		}
	}
	if len(sections) == 0 {
		return "", nil, nil, missing
	}
	return body.String(), sections, epClaimSpans(doc.Claims, claimsStart), missing
}

func epClaimsText(claims []epClaim) string {
	parts := []string{}
	for _, claim := range claims {
		if text := textHTML(claim.Inner); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// epClaimSpans: зависимость — только по ссылке на пункт, формулировка разнится у авторов и языков.
func epClaimSpans(claims []epClaim, offset int) []patentClaim {
	if offset < 0 {
		return nil
	}
	out := []patentClaim{}
	at := offset
	for i, claim := range claims {
		text := textHTML(claim.Inner)
		if text == "" {
			continue
		}
		if len(out) > 0 {
			at++
		}
		number, err := strconv.Atoi(strings.TrimSpace(claim.Num))
		if err != nil || number < 1 {
			number = i + 1
		}
		length := utf8.RuneCountInString(text)
		out = append(out, patentClaim{Number: number, Dependent: epDependent(text, number),
			Start: at, End: at + length})
		at += length
	}
	return out
}

var epClaimReference = regexp.MustCompile(`(?i)(?:claims?|anspr[uü]che?n?|revendications?)\s+(\d{1,3})`)

func epDependent(text string, number int) bool {
	for _, reference := range epClaimReference.FindAllStringSubmatch(text, -1) {
		if value, err := strconv.Atoi(reference[1]); err == nil && value < number {
			return true
		}
	}
	return false
}

func epDay(value, fallback string) string {
	for _, candidate := range []string{strings.TrimSpace(value), strings.TrimSpace(fallback)} {
		if day, err := time.Parse("20060102", candidate); err == nil {
			return day.Format(time.DateOnly)
		}
	}
	return ""
}
