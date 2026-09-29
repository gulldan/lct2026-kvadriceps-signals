package application

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/openalex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/jsonx"
)

const (
	liveArxivBase     = "https://export.arxiv.org/api/query"
	liveGitHubBase    = "https://api.github.com/search/repositories"
	liveHNBase        = "https://hn.algolia.com/api/v1"
	liveArxivPage     = 2000
	liveArxivPages    = 5
	liveArxivAttempts = 5
	liveMaxBody       = 64 << 20
	liveHTTPTimeout   = 90 * time.Second
)

// liveSources держит темп на каждый источник: без токена GitHub даёт анонимно 10 запросов в минуту.
type liveSources struct {
	channelErrors map[string]error
	metadata      map[string]LivePaperMetadata
	http          *http.Client
	arxiv         string
	oai           string
	github        string
	hn            string
	oa            *openalex.Client
	// scholarCache принадлежит сервису: источники создаются на каждый поиск, а кеш нужен между поисками.
	scholarCache *liveScholarCache
	token        func(context.Context) string
	sleep        func(context.Context, time.Duration) error
	progress     func(step string, wait time.Duration)
	// patience = 0 снимает ограничение: ночной сбор не торопится и берёт полноту.
	patience time.Duration
	// scanned — размер стога, а не выдачи; ноль — запасной путь через API arXiv, где перебирать нечего.
	scanned    int
	priorRepos map[string]map[string]int
	limits     []LiveLimit
	ghDown     bool
	// emergence считается до первой темы: класс карты Юна зависит от набора. У разового поиска пуст.
	emergence LiveEmergenceMedians
	// answerer и documents пусты у ночного сбора: снимок принадлежит стенду, а документы — их владельцу.
	answerer  domain.Answerer
	documents liveDocumentReader
	owner     string
	// excerpts нужны только разовому поиску — из фрагментов лент извлекаются рыночные кандидаты; в ночном снимке
	// они умножили бы хранение на число направлений.
	excerpts bool
}

func (s *liveSources) step(name string, wait time.Duration) {
	if s.progress != nil {
		s.progress(name, wait)
	}
}

func newLiveSources(token func(context.Context) string) *liveSources {
	return &liveSources{
		http: &http.Client{Timeout: liveHTTPTimeout}, arxiv: cmp.Or(os.Getenv("ARXIV_API_URL"), liveArxivBase),
		oai:    cmp.Or(os.Getenv("ARXIV_OAI_URL"), liveOAIBase),
		github: liveGitHubBase, hn: liveHNBase,
		oa:    liveOpenAlex(),
		token: token, sleep: liveSleep, limits: []LiveLimit{}, ghDown: false,
	}
}

func liveOpenAlex() *openalex.Client {
	cfg := openalex.FromEnv("rag-test-signalgraph/1.0")
	client, err := openalex.New(cfg)
	if err != nil {
		slog.Error("openalex proxy ignored", "error", err)
		cfg.Proxy = ""
		client, _ = openalex.New(cfg)
	}
	return client
}

func liveSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("live: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

func (s *liveSources) get(ctx context.Context, endpoint string, headers map[string]string) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("live: build request: %w", err)
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("live: get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, liveMaxBody))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("live: read body: %w", err)
	}
	return body, resp.Header, resp.StatusCode, nil
}

type arxivFeed struct {
	Total   int          `xml:"totalResults"`
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID              string `xml:"id"`
	Title           string `xml:"title"`
	Published       string `xml:"published"`
	Updated         string `xml:"updated"`
	Summary         string `xml:"summary"`
	DOI             string `xml:"doi"`
	PrimaryCategory struct {
		Term string `xml:"term,attr"`
	} `xml:"primary_category"`
	Authors []string `xml:"author>name"`
}

func arxivDate(day time.Time) string { return day.Format("20060102") }

// arxivQuery добавляет к запросу семейства категории и окно дат, чтобы знаменатель был общим.
func arxivQuery(query, categories string, from, through time.Time) string {
	window := fmt.Sprintf("(%s) AND submittedDate:[%s0000 TO %s2359]", categories, arxivDate(from), arxivDate(through))
	if query == "" {
		return window
	}
	return "(" + query + ") AND " + window
}

func (s *liveSources) arxivPage(ctx context.Context, query string, start, size int) (arxivFeed, error) {
	var feed arxivFeed
	q := url.Values{"search_query": {query}, "start": {strconv.Itoa(start)}, "max_results": {strconv.Itoa(size)},
		"sortBy": {"submittedDate"}, "sortOrder": {"ascending"}}
	var last error
	var cooldown time.Duration
	for attempt := range liveArxivAttempts {
		wait := max(3*time.Second*time.Duration(attempt+1), cooldown)
		// Источник, просящий ждать дольше patience, закрыт: ждать его — подарить человеку пустой экран вместо неполного ответа.
		if s.patience > 0 && wait > s.patience {
			return feed, fmt.Errorf("arxiv: source asked to wait %s, which is longer than one search may spend",
				wait.Round(time.Second))
		}
		s.step(liveStepArxiv, wait)
		if err := s.sleep(ctx, wait); err != nil {
			return feed, err
		}
		cooldown = 0
		body, header, status, err := s.get(ctx, s.arxiv+"?"+q.Encode(), nil)
		switch {
		case err != nil:
			last = fmt.Errorf("arxiv: %w", err)
		case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
			last = fmt.Errorf("arxiv: status %d", status)
			cooldown = retryAfterHint(header, 60*time.Second)
		case status != http.StatusOK:
			last = fmt.Errorf("arxiv: status %d", status)
		case xml.Unmarshal(body, &feed) != nil:
			last = errors.New("arxiv: parse")
		default:
			return feed, nil
		}
	}
	return feed, last
}

// arxivPapers перечисляет семейство не дальше liveArxivPages страниц; усечение — оговорка, а не тайна.
func (s *liveSources) arxivPapers(ctx context.Context, query string) ([]StudyPaper, int, bool, error) {
	papers := []StudyPaper{}
	seen := map[string]bool{}
	total := 0
	for page := range liveArxivPages {
		feed, err := s.arxivPage(ctx, query, page*liveArxivPage, liveArxivPage)
		if err != nil {
			return nil, 0, false, err
		}
		total = feed.Total
		for _, entry := range feed.Entries {
			id := strings.TrimPrefix(entry.ID, "http://arxiv.org/abs/")
			if base, _, ok := strings.CutLast(id, "v"); ok && base != "" {
				id = base
			}
			if id == "" || seen[id] || len(entry.Published) < 10 {
				continue
			}
			seen[id] = true
			papers = append(papers, s.recordArxivEntry(id, entry))
		}
		if len(feed.Entries) < liveArxivPage || len(papers) >= total {
			return papers, total, false, nil
		}
	}
	return papers, total, total > len(papers), nil
}

func (s *liveSources) recordArxivEntry(id string, entry arxivEntry) StudyPaper {
	if s.metadata == nil {
		s.metadata = map[string]LivePaperMetadata{}
	}
	available := cmp.Or(entry.Updated, entry.Published)
	observed := time.Now().UTC().Format(time.RFC3339)
	s.metadata["arxiv:"+id] = LivePaperMetadata{
		Abstract: strings.Join(strings.Fields(entry.Summary), " "), DOI: strings.TrimSpace(entry.DOI),
		Category: entry.PrimaryCategory.Term, AvailableAt: available, ObservedAt: observed,
		SourceURL:   strings.Replace(entry.ID, "http://arxiv.org/", "https://arxiv.org/", 1),
		ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(entry.Title+"\n"+entry.Summary+"\n"+available))),
	}
	return StudyPaper{ID: "arxiv:" + id, Title: strings.Join(strings.Fields(entry.Title), " "),
		Date: entry.Published[:10], CanonicalID: "arxiv:" + id, ContentHash: "", URL: "https://arxiv.org/abs/" + id,
		Authors: entry.Authors, DocumentID: "", SnapshotID: ""}
}

type githubSearch struct {
	Total int `json:"total_count"`
	Items []struct {
		FullName    string `json:"full_name"`
		URL         string `json:"html_url"`
		Created     string `json:"created_at"`
		Stars       int    `json:"stargazers_count"`
		Description string `json:"description"`
		Owner       struct {
			Type string `json:"type"`
		} `json:"owner"`
	} `json:"items"`
}

var errGitHubLimited = errors.New("github: rate limited")

func (s *liveSources) githubSearch(ctx context.Context, q string, sort string, perPage int) (githubSearch, error) {
	var result githubSearch
	if s.ghDown {
		return result, errGitHubLimited
	}
	token := s.token(ctx)
	pause := 6500 * time.Millisecond
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if token != "" {
		pause = 2100 * time.Millisecond
		headers["Authorization"] = "Bearer " + token
	}
	values := url.Values{"q": {q}, "per_page": {strconv.Itoa(perPage)}}
	if sort != "" {
		values.Set("sort", sort)
		values.Set("order", "desc")
	}
	for attempt := range 2 {
		if err := s.sleep(ctx, pause); err != nil {
			return result, err
		}
		body, header, status, err := s.get(ctx, s.github+"?"+values.Encode(), headers)
		if err != nil {
			return result, fmt.Errorf("github: %w", err)
		}
		if status == http.StatusOK {
			if err := jsonx.Unmarshal(body, &result); err != nil {
				return result, fmt.Errorf("github: parse: %w", err)
			}
			return result, nil
		}
		if (status != http.StatusForbidden && status != http.StatusTooManyRequests) || attempt == 1 {
			s.ghDown = true
			return result, fmt.Errorf("%w: status %d", errGitHubLimited, status)
		}
		if err := s.sleep(ctx, retryAfterHint(header, 60*time.Second)); err != nil {
			return result, err
		}
	}
	return result, errGitHubLimited
}

func retryAfterHint(header http.Header, fallback time.Duration) time.Duration {
	if v, err := strconv.Atoi(header.Get("Retry-After")); err == nil && v > 0 {
		return min(time.Duration(v)*time.Second, 90*time.Second)
	}
	if v, err := strconv.ParseInt(header.Get("X-Ratelimit-Reset"), 10, 64); err == nil && v > 0 {
		return min(max(time.Until(time.Unix(v, 0)), 5*time.Second), 90*time.Second)
	}
	return fallback
}

type hnSearch struct {
	Hits int `json:"nbHits"`
	Rows []struct {
		ID      string `json:"objectID"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		Created string `json:"created_at"`
		Points  int    `json:"points"`
	} `json:"hits"`
}

func (s *liveSources) hnSearch(ctx context.Context, q string, from, to time.Time, top int) (hnSearch, error) {
	var result hnSearch
	path := "/search_by_date"
	if top > 0 {
		path = "/search"
	}
	filters := "created_at_i>=" + strconv.FormatInt(from.Unix(), 10)
	if !to.IsZero() {
		filters += ",created_at_i<" + strconv.FormatInt(to.Unix(), 10)
	}
	values := url.Values{"query": {q}, "tags": {"story"}, "numericFilters": {filters}, "hitsPerPage": {strconv.Itoa(top)}}
	if err := s.sleep(ctx, 300*time.Millisecond); err != nil {
		return result, err
	}
	body, _, status, err := s.get(ctx, s.hn+path+"?"+values.Encode(), nil)
	if err != nil {
		return result, fmt.Errorf("hn: %w", err)
	}
	if status != http.StatusOK {
		return result, fmt.Errorf("hn: status %d", status)
	}
	if err := jsonx.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("hn: parse: %w", err)
	}
	return result, nil
}
