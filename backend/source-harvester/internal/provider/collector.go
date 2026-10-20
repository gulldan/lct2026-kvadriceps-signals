package provider

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

// Collector импортирует снимки патентного и веб-сборщика тем же конвейером страниц, сырья, аренды и дедупликации.
type Collector struct {
	kind, endpoint string
	cnEndpoint     string
	population     string
	fetcher        *fetcher
}

const providerWeb = "web"

func NewCollector(kind string) (*Collector, error) {
	if kind != providerPatent && kind != providerWeb {
		return nil, errors.New("collector must be patent or web")
	}
	key, endpoint := "PATENT_COLLECTOR_URL", "http://patent-collector:8090"
	if kind == providerWeb {
		key, endpoint = "WEB_COLLECTOR_URL", "http://web-collector:8090"
	}
	if value := os.Getenv(key); value != "" {
		endpoint = value
	}
	parsed, e := url.Parse(endpoint)
	if e != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != schemeHTTPS) {
		return nil, errors.New("invalid internal collector URL")
	}
	cnEndpoint := ""
	if kind == providerPatent {
		cnEndpoint = strings.TrimRight(os.Getenv("CN_PATENT_COLLECTOR_URL"), "/")
	}
	if cnEndpoint != "" {
		u, e := url.Parse(cnEndpoint)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != schemeHTTPS) {
			return nil, errors.New("invalid CN collector URL")
		}
	}
	return &Collector{kind: kind, endpoint: strings.TrimSuffix(endpoint, "/"), cnEndpoint: cnEndpoint,
		fetcher: newFetcher(time.Second)}, nil
}

func (a *Collector) SetRequestGate(g Gate) { a.fetcher.gate = g }

type collectorRecord struct {
	ID            string             `json:"id"`
	DOI           string             `json:"doi"`
	Genre         string             `json:"genre"`
	KindCode      string             `json:"kind_code"`
	Title         string             `json:"title"`
	Text          string             `json:"text"`
	TextStatus    string             `json:"text_status"`
	Date          string             `json:"date"`
	DateBasis     string             `json:"date_basis"`
	URL           string             `json:"url"`
	CanonicalURL  string             `json:"canonical_url"`
	OriginalURL   string             `json:"original_url"`
	FamilyID      string             `json:"family_id"`
	FamilyBasis   string             `json:"family_basis"`
	FilingBasis   string             `json:"filing_basis"`
	Filing        string             `json:"filing"`
	Priority      string             `json:"priority"`
	PriorityBasis string             `json:"priority_basis"`
	AvailableAt   string             `json:"available_at"`
	ObservedAt    string             `json:"observed_at"`
	Language      string             `json:"language"`
	Limitations   []string           `json:"limitations"`
	Anchors       []sourcedoc.Anchor `json:"anchors"`
	FullText      *collectorFullText `json:"full_text"`
}

// collectorFullText: смещения — кодовые точки собственного текста сборщика.
type collectorFullText struct {
	Sections []sourcedoc.Section `json:"sections"`
	Claims   []collectorClaim    `json:"claims"`
	Missing  []string            `json:"missing_sections"`
}

type collectorClaim struct {
	Number int `json:"number"`
	Start  int `json:"start"`
	End    int `json:"end"`
}

// collectorSections сдвигает смещения на префикс заголовка; не влезшее отбрасывается: неверное смещение хуже никакого.
func collectorSections(row collectorRecord, text string) []sourcedoc.Section {
	if row.FullText == nil || len(row.FullText.Sections) == 0 || row.Text == "" {
		return nil
	}
	shift := utf8.RuneCountInString(text) - utf8.RuneCountInString(row.Text)
	if shift < 0 {
		return nil
	}
	sections := make([]sourcedoc.Section, 0, len(row.FullText.Sections))
	for _, section := range row.FullText.Sections {
		section.Start, section.End = section.Start+shift, section.End+shift
		sections = append(sections, section)
	}
	if sourcedoc.ValidateSections(sections, utf8.RuneCountInString(text)) != nil {
		return nil
	}
	return sections
}

func collectorAnchors(row collectorRecord, text string) []sourcedoc.Anchor {
	if row.Text == "" {
		return nil
	}
	shift := utf8.RuneCountInString(text) - utf8.RuneCountInString(row.Text)
	if shift < 0 {
		return nil
	}
	anchors := []sourcedoc.Anchor{}
	if row.FullText != nil {
		for _, claim := range row.FullText.Claims {
			anchors = append(anchors, sourcedoc.Anchor{
				Path: "claim/" + strconv.Itoa(claim.Number), Start: claim.Start + shift, End: claim.End + shift,
			})
		}
	}
	for _, anchor := range row.Anchors {
		anchor.Start, anchor.End = anchor.Start+shift, anchor.End+shift
		anchors = append(anchors, anchor)
	}
	if len(anchors) == 0 || sourcedoc.ValidateAnchors(anchors, utf8.RuneCountInString(text)) != nil {
		return nil
	}
	return anchors
}

func (a *Collector) Fetch(ctx context.Context, scope Scope, cursor string) (Page, error) {
	page := Page{Provider: a.kind, Scope: scope, Cursor: cursor, Records: []Record{}, Rejected: []Rejected{}}
	if err := scope.ValidateFor(a.kind); err != nil {
		return page, err
	}
	q := url.Values{"from": {scope.From}, "through": {scope.Through},
		"query": {scope.Query}, "limit": {strconv.Itoa(scope.PageSize)}}
	if a.kind == providerWeb && scope.WorkType == sourcedoc.GenreInvestmentThesis {
		q.Set("genre", sourcedoc.GenreInvestmentThesis)
		q.Set("include_undated", "1")
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	endpoint := a.endpoint
	if scope.Query == "country:CN" && a.cnEndpoint != "" {
		endpoint = a.cnEndpoint
	}
	raw, observed, err := a.fetcher.get(ctx, endpoint+"/v1/records?"+q.Encode())
	if err != nil {
		if e, ok := errors.AsType[*HTTPError](err); ok && e.Status == http.StatusConflict {
			return page, ErrCursorExpired
		}
		return page, err
	}
	var response struct {
		Records   []jsontext.Value `json:"records"`
		Next      string           `json:"next_cursor"`
		Exhausted bool             `json:"exhausted"`
		Total     int              `json:"total"`
		Limits    []string         `json:"limits"`
		SourceSet string           `json:"source_set"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return page, fmt.Errorf("decode collector page: %w", err)
	}
	a.population = response.SourceSet
	page.Raw, page.RawHash, page.Observed = raw, digest(raw), observed
	page.Next, page.Exhausted, page.Total = response.Next, response.Exhausted, response.Total
	page.Limitations = response.Limits
	page.Received = len(response.Records)
	for _, item := range response.Records {
		var row collectorRecord
		if err = json.Unmarshal(item, &row); err != nil {
			page.Rejected = append(page.Rejected, Rejected{Reason: "invalid collector record"})
			continue
		}
		normalized, e := collectorDocument(a.kind, row, item, scope, observed)
		if e != nil {
			page.Rejected = append(page.Rejected, Rejected{NativeID: row.ID, Reason: e.Error()})
			continue
		}
		page.Records = append(page.Records, normalized)
	}
	return page, page.Validate(scope.PageSize)
}
func collectorPatentDates(source *sourcedoc.Source, row collectorRecord) {
	// CN A — заявки, а не выдачи. Старые снимки бюллетеня USPTO без kind_code явно называют основание по дате выдачи.
	granted := strings.HasPrefix(row.KindCode, "B") ||
		(strings.HasPrefix(row.ID, "CN") && (row.KindCode == "C" || row.KindCode == "U" || row.KindCode == "S")) ||
		(strings.HasPrefix(row.ID, "US") && strings.HasPrefix(row.DateBasis, "USPTO issue"))
	if source.Kind == providerPatent && source.Publication != nil && granted {
		grant := *source.Publication
		grant.Basis = cmp.Or(row.DateBasis, "USPTO issue date")
		source.Grant = &grant
	}
	if row.Filing != "" {
		source.Filing = &sourcedoc.Date{Value: row.Filing, Precision: precisionDay,
			Basis: cmp.Or(row.FilingBasis, "USPTO primary document filing line")}
	}
	if row.Priority != "" {
		source.Priority = &sourcedoc.Date{Value: row.Priority, Precision: precisionDay,
			Basis: cmp.Or(row.PriorityBasis, "USPTO primary document priority claim")}
	}
}

func collectorCanonical(kind string, row collectorRecord) (string, error) {
	if kind == providerPatent {
		if row.FamilyID != "" {
			return "patent-family:" + row.FamilyID, nil
		}
		return "patent:" + row.ID, nil
	}
	if row.DOI != "" {
		doi := strings.ToLower(strings.TrimSpace(row.DOI))
		if !strings.HasPrefix(doi, "10.") || !strings.Contains(doi, "/") || strings.ContainsAny(doi, " \t\r\n") {
			return "", errors.New("invalid collector DOI")
		}
		// Одна публикация из нескольких каталогов сохраняет общую идентичность.
		return "doi:" + doi, nil
	}
	return cmp.Or(row.OriginalURL, row.CanonicalURL, row.URL), nil
}

func collectorDocument(kind string, row collectorRecord, raw jsontext.Value, scope Scope, observed time.Time) (Record, error) {
	result := Record{Title: row.Title, TextStatus: row.TextStatus, Raw: raw}
	text := strings.TrimSpace(row.Title + "\n\n" + row.Text)
	if row.Title == "" || len(row.Title) > 4096 || len(text) > 512<<10 {
		return result, errors.New("invalid collector title/text")
	}
	canonical, err := collectorCanonical(kind, row)
	if err != nil {
		return result, err
	}
	sourceKind := "analysis"
	if kind == providerPatent {
		sourceKind = providerPatent
	}
	source := sourcedoc.Source{Version: 1, Kind: sourceKind, Provider: kind, NativeID: row.ID, URL: row.URL,
		CanonicalID: canonical, Publication: &sourcedoc.Date{Value: row.Date, Precision: precisionDay, Basis: row.DateBasis},
		ObservedAt: observed, Revision: digest([]byte(text)), ContentHash: digest([]byte(text)),
		HashAlgorithm: hashSHA256, AccessScope: scopePublic}
	source.Publication.Basis = cmp.Or(source.Publication.Basis, "collector publisher record")
	if err = collectorInvestorContext(&source, row, scope); err != nil {
		return result, err
	}
	if at, e := time.Parse(time.RFC3339, row.AvailableAt); e == nil && !at.After(source.ObservedAt) {
		source.AvailableAt = new(at)
		source.AvailableBasis = "first successful source observation"
	}
	collectorPatentDates(&source, row)
	if e := scopeDate(source.Publication, scope); e != nil {
		return result, e
	}
	if row.Text == "" {
		result.TextStatus = "metadata_only"
		result.Limitations = append(result.Limitations, "full_text_unavailable; only saved title and publisher metadata are evidence")
	}
	source.Sections = collectorSections(row, text)
	source.Anchors = collectorAnchors(row, text)
	if language := normalizeProviderLanguage(row.Language); language != "" {
		source.Language, source.LanguageBasis = language, "collector provider language field"
	}
	if len(source.Sections) == 1 && source.Sections[0].Name == "abstract" {
		result.TextStatus = "abstract_only"
		result.Limitations = append(result.Limitations,
			"abstract_only; the provider returned no section of this document beyond its abstract")
	}
	if row.FullText != nil && len(row.FullText.Missing) > 0 {
		result.Limitations = append(result.Limitations,
			"sections_partial; the provider returned no "+strings.Join(row.FullText.Missing, ", "))
	}
	if row.TextStatus == "representative_claim" {
		result.Limitations = append(result.Limitations, "representative_claim_only; full patent specification is not loaded")
	}
	// Сборщик, прочитавший документ частично, говорит об этом: отсутствующий раздел — не пустой.
	result.Limitations = append(result.Limitations, row.Limitations...)
	source.TextStatus = result.TextStatus
	revision, err := collectorRevision(source, raw)
	if err != nil {
		return result, err
	}
	source.Revision = revision
	if err := source.Validate(); err != nil {
		return result, err
	}
	result.Source = source
	result.Text = text
	return result, nil
}

func (a *Collector) PopulationVersion() string { return a.population }
