package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const patentFullTextVersion = "uspto-public-search-fulltext-v1"
const patentFullText = "full_text"

const sectionClaims = "claims"

// В patentHighlight раздел, которого у документа нет, приходит null — это факт о патенте.
type patentHighlight struct {
	GUID            string   `json:"guid"`
	CompositeID     string   `json:"compositeId"`
	PageCount       int      `json:"pageCount"`
	Language        string   `json:"languageIndicator"`
	Abstract        *string  `json:"abstractHtml"`
	Brief           *string  `json:"briefHtml"`
	Background      *string  `json:"backgroundTextHtml"`
	Description     *string  `json:"descriptionHtml"`
	Claims          *string  `json:"claimsHtml"`
	Assignees       []string `json:"assigneeName"`
	Applicants      []string `json:"applicantName"`
	CPC             string   `json:"cpcInventiveFlattened"`
	USReferences    []string `json:"usRefGroup"`
	ForeignRefGroup []string `json:"foreignRefGroup"`
	Kind            []string `json:"kindCode"`
	Type            string   `json:"type"`
	Title           string   `json:"inventionTitle"`
}

var patentClaimNumber = regexp.MustCompile(`(?:^|\s)(\d{1,3})\s*\.\s`)
var patentClaimReference = regexp.MustCompile(`claims?\s+(\d{1,3})`)

func patentGUID(id, kind string) string {
	number := strings.TrimPrefix(normalizedPatentID(id), "US")
	if number == "" || kind == "" {
		return ""
	}
	return "US-" + number + "-" + kind
}

func (c *patentSearchClient) highlight(ctx context.Context, guid string) (patentHighlight, []byte, error) {
	var page patentHighlight
	path := "/api/patents/highlight/" + guid + "?queryId=0&source=USPAT&includeSections=true&uniqueId=1"
	if c.token == "" {
		if err := c.session(ctx); err != nil {
			return page, nil, fmt.Errorf("USPTO full text: %w", err)
		}
	}
	body, _, err := c.get(ctx, path)
	if failure, ok := errors.AsType[*sourceHTTPError](err); ok && failure.Status == http.StatusUnauthorized {
		// Один раз обновляем сессию, как публичное приложение.
		c.token = ""
		if err = c.session(ctx); err == nil {
			body, _, err = c.get(ctx, path)
		}
	}
	if err != nil {
		return page, nil, err
	}
	if err = json.Unmarshal(body, &page); err != nil {
		return page, body, fmt.Errorf("decode USPTO full text: %w", err)
	}
	if page.GUID != guid || page.Type != patentSearchSource {
		return page, body, errors.New("USPTO full text belongs to another document")
	}
	return page, body, nil
}

func htmlSection(raw *string) string {
	if raw == nil {
		return ""
	}
	return textHTML(*raw)
}

// patentClaims: зависимость — по ссылке на пункт; байтовые позиции переводятся в кодовые точки.
func patentClaims(text string, offset int) []patentClaim {
	positions := patentClaimNumber.FindAllStringSubmatchIndex(text, -1)
	claims := []patentClaim{}
	expected := 1
	for i, match := range positions {
		number, err := strconv.Atoi(text[match[2]:match[3]])
		if err != nil || number != expected {
			continue
		}
		expected++
		end := len(text)
		for _, next := range positions[i+1:] {
			if value, convErr := strconv.Atoi(text[next[2]:next[3]]); convErr == nil && value == expected {
				end = next[2]
				break
			}
		}
		body := text[match[3]:end]
		dependent := false
		for _, reference := range patentClaimReference.FindAllStringSubmatch(body, -1) {
			if value, convErr := strconv.Atoi(reference[1]); convErr == nil && value < number {
				dependent = true
				break
			}
		}
		claims = append(claims, patentClaim{Number: number, Dependent: dependent,
			Start: offset + utf8.RuneCountInString(text[:match[2]]), End: offset + utf8.RuneCountInString(text[:end])})
	}
	return claims
}

// patentSections называет отсутствующий раздел, а не хранит пустую строку.
func patentSections(page patentHighlight) (string, []patentSection, []patentClaim, []string) {
	ordered := []struct {
		name string
		text string
	}{
		{sectionAbstract, htmlSection(page.Abstract)},
		{"background", htmlSection(page.Background)},
		{"summary", htmlSection(page.Brief)},
		{"description", htmlSection(page.Description)},
		{sectionClaims, htmlSection(page.Claims)},
	}
	var body strings.Builder
	sections := []patentSection{}
	claims := []patentClaim{}
	missing := []string{}
	// runes — кодовые точки, уже набранные в builder; body.Len() дал бы байты.
	runes := 0
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
			claims = patentClaims(section.text, start)
		}
	}
	return body.String(), sections, claims, missing
}

// applyPatentFullText сохраняет текст с неполными разделами и называет не отданные провайдером.
func applyPatentFullText(row record, page patentHighlight, body provenance, observed string) (record, error) {
	text, sections, claims, missing := patentSections(page)
	if text == "" || len(claims) == 0 {
		return row, errors.New("USPTO full text carries no readable sections or claims")
	}
	row.Text = text
	row.TextStatus = patentFullText
	row.ContentHash = hashBytes([]byte(text))
	row.ObservedAt = observed
	row.Language = page.Language
	row.FullText = &patentFullTextFacts{
		GUID: page.GUID, CompositeID: page.CompositeID, PageCount: page.PageCount,
		Sections: sections, Claims: claims, Assignees: page.Assignees, Applicants: page.Applicants,
		CPC: splitPatentCPC(page.CPC), USReferences: page.USReferences,
		ForeignReferences: page.ForeignRefGroup, Missing: missing,
	}
	if len(missing) > 0 {
		row.Limitations = appendLimitation(row.Limitations,
			"patent_sections_partial; provider returned no "+strings.Join(missing, ", "))
	}
	if !parserIncludes(row.ParserVersion, patentFullTextVersion) {
		row.ParserVersion = strings.Trim(row.ParserVersion+";"+patentFullTextVersion, ";")
	}
	row.Provenance = append(row.Provenance, body)
	return row, nil
}

func splitPatentCPC(value string) []string {
	codes := []string{}
	for code := range strings.SplitSeq(value, ";") {
		if code = strings.TrimSpace(code); code != "" {
			codes = append(codes, code)
		}
	}
	return codes
}

// patentFullTexts не запрашивает отозванный номер: это окончательный ответ.
func (s *service) patentFullTexts(
	ctx context.Context, client *patentSearchClient, data *snapshot, progress *patentMetadataProgress,
) error {
	for i := range data.Records {
		row := data.Records[i]
		if row.TextStatus == patentFullText || row.ExcludedReason != "" || !metadataVerified(row) ||
			!candidatePatent(row, s.config.PatentTerms) {
			continue
		}
		guid := patentGUID(row.ID, row.KindCode)
		if guid == "" {
			continue
		}
		page, body, err := client.highlight(ctx, guid)
		if failure, ok := errors.AsType[*sourceHTTPError](err); ok && failure.Status == http.StatusNotFound {
			// Public Search отдаёт разделы лишь меньшинству выданных патентов: отказ — окончательный ответ
			// про документ, а не сбой прохода; метаданные остаются.
			data.Records[i].Limitations = appendLimitation(data.Records[i].Limitations,
				"full_text_unavailable; USPTO Public Search holds no sectioned document for "+guid)
			progress.FullTextMissing++
			continue
		}
		if err != nil && body != nil {
			// Ответ получен, но не читается или описывает другой документ: записи идут в постоянном порядке,
			// и остановка на нём не давала бы ни одного полного текста после него ни в одном проходе.
			data.Records[i].Limitations = appendLimitation(data.Records[i].Limitations,
				"full_text_unreadable; "+guid+": "+err.Error())
			continue
		}
		if err != nil {
			return err
		}
		enriched, applyErr := applyPatentFullText(row, page,
			provenance{URL: client.base + "/api/patents/highlight/" + guid, SHA256: hashBytes(body)}, data.ObservedAt)
		if applyErr != nil {
			data.Records[i].Limitations = appendLimitation(data.Records[i].Limitations, applyErr.Error())
			continue
		}
		data.Records[i] = enriched
		progress.FullText++
	}
	return nil
}

func appendLimitation(limitations []string, value string) []string {
	if slices.Contains(limitations, value) {
		return limitations
	}
	return append(limitations, value)
}
