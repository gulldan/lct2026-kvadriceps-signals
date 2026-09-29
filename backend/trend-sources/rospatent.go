package main

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Поисковая платформа Роспатента: ключ Bearer из кабинета, запросы в духе Elasticsearch, JSON.
// Квоты спецификация не называет, поэтому темп общий — раз в секунду — и остановка на первом отказе.
const (
	rospatentBase          = "https://searchplatform.rospatent.gov.ru/patsearch/v0.2"
	rospatentSource        = "Rospatent"
	rospatentParserVersion = "rospatent-patsearch-v0.2-v1"
	rospatentPage          = 100
	rospatentDataset       = "ru_since_1994"
)

func rospatentKey() string { return strings.TrimSpace(os.Getenv("ROSPATENT_API_KEY")) }

func (s *service) rospatentBase() string { return cmp.Or(s.rospatentURL, rospatentBase) }

type rospatentName struct {
	Name string `json:"name"`
}

type rospatentBiblio struct {
	Title     string          `json:"title"`
	Applicant []rospatentName `json:"applicant"`
	Patentee  []rospatentName `json:"patentee"`
}

type rospatentHit struct {
	Common struct {
		DocumentNumber   string `json:"document_number"`
		PublishingOffice string `json:"publishing_office"`
		Kind             string `json:"kind"`
		PublicationDate  string `json:"publication_date"`
		Application      struct {
			Number     string `json:"number"`
			FilingDate string `json:"filing_date"`
		} `json:"application"`
		Priority       any `json:"priority"`
		Classification struct {
			IPC []struct {
				Fullname string `json:"fullname"`
			} `json:"ipc"`
			CPC []struct {
				Fullname string `json:"fullname"`
			} `json:"cpc"`
		} `json:"classification"`
		Family struct {
			DocdbFamilyID any `json:"docdb_family_id"`
		} `json:"family"`
	} `json:"common"`
	Biblio   map[string]rospatentBiblio `json:"biblio"`
	Abstract map[string]string          `json:"abstract"`
}

type rospatentSearch struct {
	Total     int            `json:"total"`
	Available int            `json:"available"`
	Hits      []rospatentHit `json:"hits"`
}

// rospatentDay: платформа пишет даты как 2009.06.20 в поиске и 20090620 в документах.
func rospatentDay(value string) string {
	for _, layout := range []string{"2006.01.02", "20060102", time.DateOnly} {
		if day, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return day.Format(time.DateOnly)
		}
	}
	return ""
}

// rospatentDocID — идентификатор платформы: {страна}{номер}{вид}_{дата публикации YYYYMMDD}.
func rospatentDocID(hit rospatentHit) string {
	day := rospatentDay(hit.Common.PublicationDate)
	if day == "" {
		return ""
	}
	return hit.Common.PublishingOffice + hit.Common.DocumentNumber + hit.Common.Kind + "_" + strings.ReplaceAll(day, "-", "")
}

// rospatentRecord предпочитает английские название и реферат — термины радара английские; язык записи указан.
func rospatentRecord(hit rospatentHit, observedAt string) (record, bool) {
	date := rospatentDay(hit.Common.PublicationDate)
	id := hit.Common.PublishingOffice + hit.Common.DocumentNumber + hit.Common.Kind
	if hit.Common.DocumentNumber == "" || hit.Common.PublishingOffice == "" || date == "" {
		return record{}, false
	}
	lang := "en"
	if strings.TrimSpace(hit.Biblio["en"].Title) == "" {
		lang = "ru"
	}
	row := record{ID: id, KindCode: hit.Common.Kind, Title: strings.Join(strings.Fields(hit.Biblio[lang].Title), " "), Date: date,
		DateBasis: "Rospatent publication date", TitleBasis: "Rospatent bibliographic title (" + lang + ")",
		TextStatus: metadataOnlyText, ParserVersion: rospatentParserVersion, ObservedAt: observedAt, AvailableAt: observedAt,
		URL: "https://searchplatform.rospatent.gov.ru/doc/" + rospatentDocID(hit), Source: rospatentSource, Language: lang,
		Application: strings.TrimSpace(hit.Common.Application.Number)}
	if row.Title == "" {
		return record{}, false
	}
	if day := rospatentDay(hit.Common.Application.FilingDate); day != "" {
		row.Filing, row.FilingBasis = day, "Rospatent application filing date"
	}
	for _, claim := range opsList(hit.Common.Priority) {
		node := opsMap(claim)
		if number := opsText(node["number"]); number != "" {
			row.PriorityClaims = append(row.PriorityClaims, opsText(node["country"])+number)
		}
		if day := rospatentDay(opsText(node["filing_date"])); day != "" && (row.Priority == "" || day < row.Priority) {
			row.Priority, row.PriorityBasis = day, "Rospatent earliest priority claim"
		}
	}
	switch family := rospatentFamily(hit.Common.Family.DocdbFamilyID); {
	case family != "":
		row.FamilyID, row.FamilyBasis = "docdb:"+family, opsFamilyBasis
	case row.Application != "":
		row.FamilyID, row.FamilyBasis = "application:RU"+row.Application, "Rospatent application number"
	}
	if text := strings.TrimSpace(textHTML(cmp.Or(hit.Abstract[lang], hit.Abstract["ru"]))); text != "" {
		row.Text, row.TextStatus = text, opsAbstractStatus
		row.FullText = &patentFullTextFacts{
			Sections: []patentSection{{Name: sectionAbstract, Start: 0, End: utf8.RuneCountInString(text)}},
			Claims:   []patentClaim{}, Applicants: rospatentNames(hit.Biblio[lang]), CPC: rospatentSymbols(hit)}
	}
	return row, true
}

// rospatentFamily: DOCDB-семейство приходит числом или строкой; ноль и пустое — семьи нет.
func rospatentFamily(value any) string {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return strconv.FormatFloat(v, 'f', 0, 64)
		}
	case string:
		if v != "" && v != "0" {
			return v
		}
	}
	return ""
}

func rospatentNames(biblio rospatentBiblio) []string {
	out := []string{}
	for _, party := range append(biblio.Applicant, biblio.Patentee...) {
		if name := strings.Join(strings.Fields(party.Name), " "); name != "" && !containsString(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func rospatentSymbols(hit rospatentHit) []string {
	out := []string{}
	for _, symbol := range hit.Common.Classification.CPC {
		if symbol.Fullname != "" && !containsString(out, symbol.Fullname) {
			out = append(out, symbol.Fullname)
		}
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// rospatentSearch: 401 — ключ; 429 и 5xx останавливают обход на курсоре; 400 — сам запрос, термин пропускается.
func (s *service) rospatentSearch(ctx context.Context, term string, month period, offset int) ([]byte, error) {
	last, _ := time.Parse(time.DateOnly, month.End)
	body, err := json.Marshal(map[string]any{
		"q": rospatentQueryTerm(term), "limit": rospatentPage, "offset": offset, "datasets": []string{rospatentDataset},
		"sort": "publication date:desc",
		"filter": map[string]any{"date_published": map[string]any{"range": map[string]string{
			"gte": strings.ReplaceAll(month.Start, "-", ""), "lte": last.AddDate(0, 0, -1).Format("20060102")}}}})
	if err != nil {
		return nil, fmt.Errorf("encode Rospatent query: %w", err)
	}
	raw, header, status, err := s.request(ctx, http.MethodPost, s.rospatentBase()+"/search", map[string]string{
		headerAuthorization: "Bearer " + rospatentKey(), "Content-Type": contentJSON, "Accept": contentJSON}, body)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, fmt.Errorf("rospatent rejected the API key (HTTP %d)", status)
	case status == http.StatusTooManyRequests:
		return nil, errors.New("rospatent rate limit reached (Retry-After " +
			cmp.Or(header.Get("Retry-After"), "unknown") + "); the sweep continues from the cursor")
	case status == http.StatusBadRequest:
		return nil, fmt.Errorf("%w: rospatent search returned HTTP 400: %s", errQueryRejected,
			strings.TrimSpace(string(raw[:min(200, len(raw))])))
	case status != http.StatusOK:
		return nil, fmt.Errorf("rospatent search returned HTTP %d: %s", status,
			strings.TrimSpace(string(raw[:min(200, len(raw))])))
	}
	return raw, nil
}
