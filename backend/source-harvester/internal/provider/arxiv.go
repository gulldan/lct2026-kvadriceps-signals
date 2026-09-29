package provider

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

// ArXiv разносит запросы всех областей одного экземпляра не меньше чем на три секунды.
type ArXiv struct {
	base     string
	fetcher  *fetcher
	oaiMu    sync.Mutex
	oaiCache arxivOAICache
	// mirror — массовая выгрузка PDF в Google Cloud, которую arXiv сам предлагает для скачивания: предел «запрос
	// в три секунды» к ней не относится, поэтому mirrorHTTP идёт мимо общего ограничителя и файлы качаются параллельно.
	mirror     string
	mirrorHTTP *http.Client
}

// NewArXiv берёт официальный адрес API и публичный темп arXiv.
func NewArXiv() *ArXiv {
	f := newFetcher(3 * time.Second)
	// Общий шлюз ждёт ответ arXiv до 90 с плюс паузу темпа: клиент не должен отменить запрос раньше шлюза.
	f.http.Timeout = 2 * time.Minute
	f.accept = "*/*" // Один клиент с общим темпом качает и XML метаданных, и PDF версий.
	// Сеть стенда рвёт HTTP/2 и подвешивает соединения: файл до 13 МБ приходит за 0,2–1,5 с, зависший — и за минуту нет.
	// Отсюда HTTP/1.1 и короткий срок: при 20 с каждая страница ждала свой зависший файл, и область шла 20–35 с.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	mirror := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, Protocols: protocols},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("source redirects are not followed")
		},
	}
	return &ArXiv{base: cmp.Or(os.Getenv("ARXIV_API_URL"), "https://oaipmh.arxiv.org/oai"), fetcher: f,
		mirror: cmp.Or(os.Getenv("ARXIV_PDF_MIRROR"), arxivPDFMirror), mirrorHTTP: mirror}
}

// arxivPDFMirror — корень PDF выгрузки (<yymm>/<id>v<n>.pdf); текущий месяц появляется в ней поздно, его PDF — с arxiv.org.
const arxivPDFMirror = "https://storage.googleapis.com/arxiv-dataset/arxiv/arxiv/pdf"

type atomEntry struct {
	ID        string    `json:"id"          xml:"id"`
	Title     string    `json:"title"       xml:"title"`
	Summary   string    `json:"summary"     xml:"summary"`
	Published time.Time `json:"published"   xml:"published"`
	Updated   time.Time `json:"updated"     xml:"updated"`
	DOI       string    `json:"doi"         xml:"http://arxiv.org/schemas/atom doi"`
	Journal   string    `json:"journal_ref" xml:"http://arxiv.org/schemas/atom journal_ref"`
	Authors   []struct {
		Name string `json:"name" xml:"name"`
	} `json:"authors" xml:"author"`
	Categories []struct {
		Term string `json:"term" xml:"term,attr"`
	} `json:"categories" xml:"category"`
}

var arxivVersionID = regexp.MustCompile(`^(.+)v([1-9][0-9]*)$`)

// Fetch по умолчанию читает OAI-PMH; явно заданный Atom /api/query тоже работает, форматы курсоров не смешиваются.
func (a *ArXiv) Fetch(ctx context.Context, scope Scope, cursor string) (Page, error) {
	if err := scope.ValidateFor(providerArXiv); err != nil {
		return Page{}, err
	}
	endpoint, err := url.Parse(a.base)
	if err != nil {
		return Page{}, errors.New("invalid arxiv metadata endpoint")
	}
	if strings.TrimRight(endpoint.Path, "/") == "/oai" {
		return a.fetchOAI(ctx, scope, cursor)
	}
	return a.fetchAtom(ctx, scope, cursor)
}

func (a *ArXiv) fetchAtom(ctx context.Context, scope Scope, cursor string) (Page, error) {
	if strings.ContainsAny(scope.Query, "\"\r\n") {
		return Page{}, errors.New("arxiv phrase query cannot contain quotes or line breaks")
	}
	if cursor == "" {
		cursor = "0"
	}
	start, err := strconv.Atoi(cursor)
	if err != nil || start < 0 || start >= 30000 {
		return Page{}, errors.New("invalid or unsupported arxiv page cursor")
	}
	from, through := strings.ReplaceAll(scope.From, "-", ""), strings.ReplaceAll(scope.Through, "-", "")
	query := atomPhrases(scope.Query) + " AND submittedDate:[" + from + "0000 TO " + through + "2359]"
	if scope.Category != "" {
		query = "cat:" + scope.Category + " AND " + query
	}
	q := url.Values{
		"search_query": {query},
		"sortBy":       {"submittedDate"},
		"sortOrder":    {"ascending"},
		"start":        {cursor},
		"max_results":  {strconv.Itoa(scope.PageSize)},
	}
	raw, observed, err := a.fetcher.get(ctx, a.base+"?"+q.Encode())
	if err != nil {
		return Page{}, err
	}
	return decodeArxiv(raw, observed, scope, start)
}

// atomPhrases склеивает фразы области через « OR », как в OAI-PMH: один запрос вместо десятка. На недельных
// окнах 70–80 % запросов по одной фразе приходили пустыми, а каждый стоил три секунды общего предела arXiv.
func atomPhrases(query string) string {
	terms := []string{}
	for term := range strings.SplitSeq(query, " OR ") {
		if term = strings.TrimSpace(term); term != "" {
			terms = append(terms, `all:"`+term+`"`)
		}
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return "(" + strings.Join(terms, " OR ") + ")"
}

func decodeArxiv(raw []byte, observed time.Time, scope Scope, start int) (Page, error) {
	// Декодер Go не тянет внешние сущности; директивы отвергаются тоже, чтобы DTD/XXE не сошёл за ленту Atom.
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Page{}, fmt.Errorf("invalid arxiv XML: %w", err)
		}
		if _, ok := token.(xml.Directive); ok {
			return Page{}, errors.New("arxiv XML directives are forbidden")
		}
	}
	var feed struct {
		XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
		Total   *int        `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
		Start   *int        `xml:"http://a9.com/-/spec/opensearch/1.1/ startIndex"`
		Entries []atomEntry `xml:"entry"`
	}
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return Page{}, fmt.Errorf("decode arxiv feed: %w", err)
	}
	if feed.Total == nil || feed.Start == nil || *feed.Total < 0 || *feed.Start != start || len(feed.Entries) > scope.PageSize {
		return Page{}, errors.New("invalid arxiv page envelope or offset")
	}
	exhausted := start+len(feed.Entries) >= *feed.Total
	if !exhausted && len(feed.Entries) < scope.PageSize {
		return Page{}, errors.New("arxiv page truncated before totalResults")
	}
	p := Page{
		Provider:  providerArXiv,
		Scope:     scope,
		Cursor:    strconv.Itoa(start),
		Total:     *feed.Total,
		Received:  len(feed.Entries),
		Exhausted: exhausted,
		Observed:  observed,
		Raw:       raw,
		RawHash:   digest(raw),
	}
	if !exhausted {
		p.Next = strconv.Itoa(start + len(feed.Entries))
	}
	for _, entry := range feed.Entries {
		record, err := arxivRecord(entry, observed, scope)
		if err != nil {
			p.Rejected = append(p.Rejected, Rejected{NativeID: entry.ID, Reason: err.Error()})
			continue
		}
		p.Records = append(p.Records, record)
	}
	return p, nil
}

func arxivRecord(e atomEntry, observed time.Time, scope Scope) (Record, error) {
	if scope.Category != "" {
		matched := false
		for _, c := range e.Categories {
			matched = matched || c.Term == scope.Category
		}
		if !matched {
			return Record{}, errors.New("arxiv entry does not match the requested category")
		}
	}
	u, err := url.Parse(e.ID)
	if err != nil || u.Hostname() != "arxiv.org" || !strings.HasPrefix(u.Path, "/abs/") || u.RawQuery != "" || u.Fragment != "" {
		return Record{}, errors.New("arxiv entry has invalid version URL")
	}
	id := strings.TrimPrefix(u.Path, "/abs/")
	parts := arxivVersionID.FindStringSubmatch(id)
	if parts == nil {
		return Record{}, errors.New("arxiv entry lacks an explicit version")
	}
	version, err := strconv.Atoi(parts[2])
	if err != nil {
		return Record{}, errors.New("invalid arxiv version number")
	}
	publication := &sourcedoc.Date{
		Value:     e.Published.UTC().Format(time.DateOnly),
		Precision: precisionDay,
		Basis:     "arXiv Atom published (v1)",
	}
	if dateErr := scopeDate(publication, scope); dateErr != nil {
		return Record{}, dateErr
	}
	if e.Updated.UTC().Format(time.DateOnly) > scope.Through {
		return Record{}, errors.New("arxiv revision appeared after cutoff")
	}
	title, abstract := strings.Join(strings.Fields(e.Title), " "), strings.Join(strings.Fields(e.Summary), " ")
	if title == "" {
		return Record{}, errors.New("arxiv entry lacks a title")
	}
	text, textStatus, sections := paperText(title, abstract)
	limitations := []string{"preprint_not_verified_peer_review", "pdf_not_fetched"}
	if abstract == "" {
		limitations = append(limitations, "abstract_unavailable")
	}
	source := sourcedoc.Source{
		Version:     1,
		Kind:        "paper",
		Provider:    providerArXiv,
		NativeID:    parts[1],
		URL:         "https://arxiv.org/abs/" + id,
		CanonicalID: "arxiv:" + parts[1],
		Publication: publication,
		RevisionPublication: &sourcedoc.Date{
			Value:     e.Updated.UTC().Format(time.DateOnly),
			Precision: precisionDay,
			Basis:     "arXiv Atom updated for this version",
		},
		ObservedAt:     observed,
		Revision:       id,
		AvailableAt:    new(e.Updated),
		AvailableBasis: "arXiv Atom updated for this version",
		ContentHash:    digest([]byte(text)),
		HashAlgorithm:  hashSHA256,
		AccessScope:    scopePublic,
		Sections:       sections,
		ArXiv: &sourcedoc.ArXiv{
			BaseID:         parts[1],
			Version:        version,
			FirstPublished: e.Published,
			VersionUpdated: e.Updated,
			DOI:            e.DOI,
			JournalRef:     e.Journal,
		},
	}
	if validateErr := source.Validate(); validateErr != nil {
		return Record{}, validateErr
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return Record{}, fmt.Errorf("encode arxiv entry: %w", err)
	}
	return Record{
		Source:      source,
		Title:       title,
		Text:        text,
		TextStatus:  textStatus,
		Raw:         raw,
		Limitations: limitations,
		ContentURL:  "https://arxiv.org/pdf/" + id,
	}, nil
}
