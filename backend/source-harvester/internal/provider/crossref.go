package provider

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

const providerCrossref = "crossref"

// Crossref — один экземпляр на воркер: темп запросов держит его fetcher.
type Crossref struct {
	base    string
	mailto  string
	fetcher *fetcher
	papers  *europePMC
}

// ErrCursorExpired требует нового полного обхода окна перекрытия, а не пропуска страницы.
var ErrCursorExpired = errors.New("provider rejected saved cursor; restart the fixed overlap window")

// NewCrossref: mailto включает «вежливый пул» Crossref.
func NewCrossref(mailto string) *Crossref {
	return &Crossref{base: "https://api.crossref.org/works", mailto: mailto,
		fetcher: newFetcher(time.Second), papers: newEuropePMC()}
}

type crossrefDate struct {
	Parts [][]int `json:"date-parts"`
}
type crossrefWork struct {
	DOI             string       `json:"DOI"`
	Title           []string     `json:"title"`
	Abstract        string       `json:"abstract"`
	Type            string       `json:"type"`
	PublishedOnline crossrefDate `json:"published-online"`
	Published       crossrefDate `json:"published"`
	Issued          crossrefDate `json:"issued"`
}

func (c *Crossref) Fetch(ctx context.Context, scope Scope, cursor string) (Page, error) {
	if err := scope.ValidateFor(providerCrossref); err != nil {
		return Page{}, err
	}
	if !slices.Contains([]string{"journal-article", "proceedings-article", "posted-content"}, scope.WorkType) {
		return Page{}, errors.New("Crossref work_type must be journal-article, proceedings-article or posted-content")
	}
	if cursor == "" {
		cursor = "*"
	}
	if len(cursor) > 16384 {
		return Page{}, errors.New("Crossref cursor exceeds 16 KiB")
	}
	filter := "from-pub-date:" + scope.From + ",until-pub-date:" + scope.Through + ",type:" + scope.WorkType
	q := url.Values{"query.title": {scope.Query}, "filter": {filter},
		"sort": {"score"}, "order": {"desc"}, "rows": {strconv.Itoa(scope.PageSize)}, "cursor": {cursor}}
	if c.mailto != "" {
		q.Set("mailto", c.mailto)
	}
	raw, observed, err := c.fetcher.get(ctx, c.base+"?"+q.Encode())
	if err != nil {
		if httpErr, ok := errors.AsType[*HTTPError](err); ok && httpErr.Status == http.StatusBadRequest && cursor != "*" {
			return Page{}, fmt.Errorf("%w: %w", ErrCursorExpired, err)
		}
		return Page{}, err
	}
	page, err := decodeCrossref(raw, observed, scope, cursor)
	if err != nil || c.papers == nil {
		return page, err
	}
	for i := range page.Records {
		if err = c.papers.enrich(ctx, &page.Records[i]); err != nil {
			return Page{}, fmt.Errorf("enrich Crossref paper: %w", err)
		}
	}
	return page, nil
}

func decodeCrossref(raw []byte, observed time.Time, scope Scope, cursor string) (Page, error) {
	var response struct {
		Status  string `json:"status"`
		Message struct {
			Total *int             `json:"total-results"`
			Next  string           `json:"next-cursor"`
			Items []jsontext.Value `json:"items"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return Page{}, fmt.Errorf("decode Crossref page: %w", err)
	}
	m := response.Message
	if response.Status != "ok" || m.Total == nil || *m.Total < 0 || m.Items == nil || len(m.Items) > scope.PageSize {
		return Page{}, errors.New("invalid Crossref page envelope")
	}
	// API может не отдать next-cursor на последней полной странице; нет курсора на первой странице
	// при большем числе работ — ответ битый.
	exhausted := len(m.Items) < scope.PageSize || m.Next == "" && (cursor != "*" || *m.Total <= len(m.Items))
	if !exhausted && (m.Next == "" || len(m.Next) > 16384) {
		return Page{}, errors.New("Crossref pagination did not advance")
	}
	p := Page{Provider: providerCrossref, Scope: scope, Cursor: cursor, Next: m.Next, Total: *m.Total,
		Received: len(m.Items), Exhausted: exhausted, Observed: observed, Raw: raw, RawHash: digest(raw)}
	if exhausted {
		p.Next = ""
	}
	for _, item := range m.Items {
		record, native, err := crossrefRecord(item, observed, scope)
		if err != nil {
			p.Rejected = append(p.Rejected, Rejected{NativeID: native, Reason: err.Error()})
			continue
		}
		p.Records = append(p.Records, record)
	}
	return p, nil
}

func crossrefRecord(raw jsontext.Value, observed time.Time, scope Scope) (Record, string, error) {
	var work crossrefWork
	if err := json.Unmarshal(raw, &work); err != nil {
		return Record{}, "", fmt.Errorf("decode work: %w", err)
	}
	doi := strings.ToLower(strings.TrimSpace(work.DOI))
	if !strings.HasPrefix(doi, "10.") || !strings.Contains(doi, "/") || strings.ContainsAny(doi, " \t\r\n") {
		return Record{}, doi, errors.New("invalid native DOI")
	}
	if work.Type != scope.WorkType || len(work.Title) == 0 || strings.TrimSpace(work.Title[0]) == "" {
		return Record{}, doi, errors.New("work lacks title or has a different source type")
	}
	publication, err := crossrefPublication(work)
	if err != nil {
		return Record{}, doi, err
	}
	if err := scopeDate(publication, scope); err != nil {
		return Record{}, doi, err
	}
	title, abstract := stripJATS(work.Title[0]), stripJATS(work.Abstract)
	text, status, sections := paperText(title, abstract)
	limitations := []string{"metadata_search_is_not_semantic_coverage", "full_text_not_fetched"}
	if abstract == "" {
		limitations = append(limitations, "abstract_unavailable")
	}
	if publication == nil {
		limitations = append(limitations, "publication_unknown")
	}
	doiURL := url.URL{Scheme: schemeHTTPS, Host: "doi.org", Path: "/" + doi}
	source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: providerCrossref, NativeID: doi, URL: doiURL.String(),
		CanonicalID: "doi:" + doi, Publication: publication, ObservedAt: observed,
		Revision:    paperRevision(text, publication),
		Sections:    sections,
		ContentHash: digest([]byte(text)), HashAlgorithm: hashSHA256, AccessScope: scopePublic}
	if err := source.Validate(); err != nil {
		return Record{}, doi, err
	}
	return Record{Source: source, Title: title, Text: text, TextStatus: status, Raw: raw, Limitations: limitations}, doi, nil
}

func crossrefPublication(w crossrefWork) (*sourcedoc.Date, error) {
	for _, field := range []struct {
		name string
		date crossrefDate
	}{{"published-online", w.PublishedOnline}, {"published", w.Published}, {"issued", w.Issued}} {
		if len(field.date.Parts) == 0 || len(field.date.Parts[0]) == 0 {
			continue
		}
		parts := field.date.Parts[0]
		if len(parts) > 3 {
			return nil, errors.New("invalid publication date-parts")
		}
		var value strings.Builder
		fmt.Fprintf(&value, "%04d", parts[0])
		for _, part := range parts[1:] {
			fmt.Fprintf(&value, "-%02d", part)
		}
		d := &sourcedoc.Date{Value: value.String(), Precision: []string{"year", "month", precisionDay}[len(parts)-1],
			Basis: "Crossref " + field.name + ".date-parts"}
		return d, d.Validate()
	}
	return nil, nil //nolint:nilnil // Publication is explicitly nullable when the provider supplies no date.
}
