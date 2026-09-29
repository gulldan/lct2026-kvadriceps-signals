package main

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// EPO Open Patent Services: библиографический поиск DOCDB по 100+ ведомствам. Доступ — OAuth client
// credentials (токен на 20 минут), квота — 4 ГБ в неделю, состояние темпа — в X-Throttling-Control.
const (
	sectionAbstract     = "abstract"
	headerAuthorization = "Authorization"
	contentJSON         = "application/json"
	opsBase             = "https://ops.epo.org/3.2"
	opsSource           = "EPO OPS"
	opsFamilyBasis      = "EPO OPS DOCDB family"
	opsParserVersion    = "epo-ops-biblio-search-v1"
	opsPage             = 100
	opsMaxResults       = 2000
	opsTokenMargin      = time.Minute
	opsDefaultPerMin    = 15
	opsAbstractStatus   = "abstract_only"
)

var opsSearchThrottle = regexp.MustCompile(`search=([a-z]+):([0-9]+)`)

type opsSession struct {
	token    string
	expires  time.Time
	nextAt   time.Time
	throttle string
}

func (s *service) opsBase() string { return cmp.Or(s.opsURL, opsBase) }

func opsCredentials() (string, string) {
	return strings.TrimSpace(os.Getenv("EPO_OPS_KEY")), strings.TrimSpace(os.Getenv("EPO_OPS_SECRET"))
}

// opsToken держит токен до минуты до истечения и не хранит ответ с токеном.
func (s *service) opsToken(ctx context.Context) (string, error) {
	if s.ops.token != "" && time.Now().Before(s.ops.expires) {
		return s.ops.token, nil
	}
	key, secret := opsCredentials()
	raw, _, status, err := s.request(ctx, http.MethodPost, s.opsBase()+"/auth/accesstoken", map[string]string{
		headerAuthorization: "Basic " + base64.StdEncoding.EncodeToString([]byte(key+":"+secret)),
		"Content-Type":      "application/x-www-form-urlencoded"}, []byte("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("EPO OPS token request returned HTTP %d", status)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if json.Unmarshal(raw, &token) != nil || token.AccessToken == "" {
		return "", errors.New("EPO OPS token response is not readable")
	}
	seconds, err := strconv.Atoi(token.ExpiresIn)
	if err != nil || seconds < 120 {
		seconds = 1200
	}
	s.ops.token, s.ops.expires = token.AccessToken, time.Now().Add(time.Duration(seconds)*time.Second-opsTokenMargin)
	return s.ops.token, nil
}

// opsThrottle читает состояние поиска из X-Throttling-Control, например «busy (images=green:100, …,
// search=green:15)»: цвет и число запросов в минуту, которое сервис принимает сейчас.
func opsThrottle(header http.Header) (string, int) {
	match := opsSearchThrottle.FindStringSubmatch(header.Get("X-Throttling-Control"))
	if match == nil {
		return "", opsDefaultPerMin
	}
	perMinute, err := strconv.Atoi(match[2])
	if err != nil || perMinute < 1 {
		perMinute = 1
	}
	return match[1], perMinute
}

// opsSearch держит темп из прошлого ответа; black, отказ и исчерпанная квота останавливают область.
func (s *service) opsSearch(ctx context.Context, cql string, start int) ([]byte, int, error) {
	if wait := time.Until(s.ops.nextAt); wait > 0 {
		select {
		case <-ctx.Done():
			return nil, 0, fmt.Errorf("EPO OPS search cancelled: %w", ctx.Err())
		case <-time.After(wait):
		}
	}
	token, err := s.opsToken(ctx)
	if err != nil {
		return nil, 0, err
	}
	values := url.Values{"q": {cql}, "Range": {fmt.Sprintf("%d-%d", start, start+opsPage-1)}}
	raw, header, status, err := s.request(ctx, http.MethodGet,
		s.opsBase()+"/rest-services/published-data/search/biblio?"+values.Encode(),
		map[string]string{headerAuthorization: "Bearer " + token, "Accept": contentJSON}, nil)
	if err != nil {
		return nil, status, err
	}
	colour, perMinute := opsThrottle(header)
	s.ops.throttle = header.Get("X-Throttling-Control")
	s.ops.nextAt = time.Now().Add(time.Minute / time.Duration(perMinute))
	switch {
	case status == http.StatusForbidden:
		return nil, status, errors.New("EPO OPS refused the request: " +
			cmp.Or(header.Get("X-Rejection-Reason"), "quota or fair-use limit") + "; the sweep continues from the cursor")
	case status == http.StatusUnauthorized:
		s.ops.token = ""
		return nil, status, errors.New("EPO OPS rejected the access token")
	case status == http.StatusNotFound:
		return nil, status, nil
	case status == http.StatusBadRequest:
		return nil, status, fmt.Errorf("%w: EPO OPS search returned HTTP 400", errQueryRejected)
	case status != http.StatusOK:
		return nil, status, fmt.Errorf("EPO OPS search returned HTTP %d", status)
	case colour == "black":
		return raw, status, errors.New("EPO OPS search service is overloaded (black); the sweep continues from the cursor")
	}
	return raw, status, nil
}

// opsCQL ищет в названии или реферате с усечением справа; даты месяца включительны с обеих сторон.
func opsCQL(term string, month period) string {
	phrase := strings.Join(strings.Fields(strings.ReplaceAll(term, `"`, " ")), " ")
	last, _ := time.Parse(time.DateOnly, month.End)
	return fmt.Sprintf(`ta="%s*" and pd within "%s %s"`, phrase, strings.ReplaceAll(month.Start, "-", ""),
		last.AddDate(0, 0, -1).Format("20060102"))
}

type opsNode = map[string]any

func opsList(v any) []any {
	switch value := v.(type) {
	case nil:
		return nil
	case []any:
		return value
	default:
		return []any{value}
	}
}

func opsMap(v any) opsNode {
	if node, ok := v.(opsNode); ok {
		return node
	}
	return opsNode{}
}

func opsText(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case opsNode:
		return opsText(value["$"])
	default:
		return ""
	}
}

func opsPick(nodes []any, attribute, want string) opsNode {
	for _, node := range nodes {
		if opsText(opsMap(node)[attribute]) == want {
			return opsMap(node)
		}
	}
	if len(nodes) > 0 {
		return opsMap(nodes[0])
	}
	return opsNode{}
}

func opsDay(value string) string {
	if day, err := time.Parse("20060102", value); err == nil {
		return day.Format(time.DateOnly)
	}
	return ""
}

func opsRecords(raw []byte, observedAt string) ([]record, int, error) {
	var root opsNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, 0, fmt.Errorf("decode EPO OPS search: %w", err)
	}
	search := opsMap(opsMap(root["ops:world-patent-data"])["ops:biblio-search"])
	total, err := strconv.Atoi(opsText(search["@total-result-count"]))
	if err != nil {
		return nil, 0, errors.New("EPO OPS search has no result count")
	}
	rows := []record{}
	for _, group := range opsList(opsMap(search["ops:search-result"])["exchange-documents"]) {
		for _, doc := range opsList(opsMap(group)["exchange-document"]) {
			if row, ok := opsRecord(opsMap(doc), observedAt); ok {
				rows = append(rows, row)
			}
		}
	}
	return rows, total, nil
}

func opsRecord(doc opsNode, observedAt string) (record, bool) {
	country, number, kind := opsText(doc["@country"]), opsText(doc["@doc-number"]), opsText(doc["@kind"])
	biblio := opsMap(doc["bibliographic-data"])
	publication := opsPick(opsList(opsMap(biblio["publication-reference"])["document-id"]), "@document-id-type",
		"docdb")
	date := opsDay(opsText(publication["date"]))
	if country == "" || number == "" || date == "" {
		return record{}, false
	}
	id := country + number + kind
	title := opsPick(opsList(biblio["invention-title"]), "@lang", "en")
	abstract := opsPick(opsList(doc["abstract"]), "@lang", "en")
	paragraphs := []string{}
	for _, p := range opsList(abstract["p"]) {
		if text := opsText(p); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	text := strings.Join(paragraphs, "\n")
	row := record{ID: id, KindCode: kind, Title: opsText(title["$"]), Text: text, Date: date,
		DateBasis:  "EPO OPS DOCDB publication date",
		TitleBasis: "EPO OPS DOCDB title (" + cmp.Or(opsText(title["@lang"]), "original") + ")",
		TextStatus: metadataOnlyText, ParserVersion: opsParserVersion, ObservedAt: observedAt, AvailableAt: observedAt,
		URL: "https://worldwide.espacenet.com/patent/search?q=pn%3D" + url.QueryEscape(id), Source: opsSource,
		Language: opsText(abstract["@lang"]), FamilyID: "docdb:" + opsText(doc["@family-id"]), FamilyBasis: opsFamilyBasis}
	if row.FamilyID == "docdb:" {
		row.FamilyID, row.FamilyBasis = "", ""
	}
	application := opsPick(opsList(opsMap(biblio["application-reference"])["document-id"]), "@document-id-type", "docdb")
	if number := opsText(application["doc-number"]); number != "" {
		row.Application = opsText(application["country"]) + number
	}
	if day := opsDay(opsText(application["date"])); day != "" {
		row.Filing, row.FilingBasis = day, "EPO OPS DOCDB application date"
	}
	for _, claim := range opsList(opsMap(biblio["priority-claims"])["priority-claim"]) {
		priority := opsPick(opsList(opsMap(claim)["document-id"]), "@document-id-type", "epodoc")
		if number := opsText(priority["doc-number"]); number != "" {
			row.PriorityClaims = append(row.PriorityClaims, number)
		}
		if day := opsDay(opsText(priority["date"])); day != "" && (row.Priority == "" || day < row.Priority) {
			row.Priority, row.PriorityBasis = day, "EPO OPS earliest priority claim"
		}
	}
	slices.Sort(row.PriorityClaims)
	row.PriorityClaims = slices.Compact(row.PriorityClaims)
	if text != "" {
		row.TextStatus = opsAbstractStatus
		row.FullText = &patentFullTextFacts{
			Sections: []patentSection{{Name: sectionAbstract, Start: 0, End: utf8.RuneCountInString(text)}},
			Claims:   []patentClaim{}, Applicants: opsApplicants(biblio), CPC: opsCPC(biblio)}
	}
	return row, true
}

func opsApplicants(biblio opsNode) []string {
	out := []string{}
	for _, applicant := range opsList(opsMap(opsMap(biblio["parties"])["applicants"])["applicant"]) {
		node := opsMap(applicant)
		if opsText(node["@data-format"]) != "epodoc" {
			continue
		}
		if name := opsText(opsMap(opsMap(node["applicant-name"])["name"])["$"]); name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func opsCPC(biblio opsNode) []string {
	out := []string{}
	for _, classification := range opsList(opsMap(biblio["patent-classifications"])["patent-classification"]) {
		node := opsMap(classification)
		if opsText(opsMap(node["classification-scheme"])["@scheme"]) != "CPCI" {
			continue
		}
		symbol := opsText(node["section"]) + opsText(node["class"]) + opsText(node["subclass"]) +
			opsText(node["main-group"]) + "/" + opsText(node["subgroup"])
		if symbol != "/" && !slices.Contains(out, symbol) {
			out = append(out, symbol)
		}
	}
	return out
}
