package pubsearch

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/main-service/internal/platform/openalex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/platform/runtimecfg"
)

const (
	SourceOpenAlex = "openalex"
	SourceCrossref = "crossref"
)

const (
	openalexBase       = openalex.DefaultBaseURL
	crossrefBase       = "https://api.crossref.org/works"
	perSourceTimeout   = 8 * time.Second
	totalBudget        = 12 * time.Second
	cacheTTL           = 15 * time.Minute
	defaultLimit       = 5
	maxLimit           = 25
	maxBodyBytes       = 4 << 20
	defaultRecentYears = 5
)

type Work struct {
	Title    string `json:"title"`
	Year     int    `json:"year"`
	DOI      string `json:"doi"`
	Venue    string `json:"venue"`
	Abstract string `json:"abstract"`
	Source   string `json:"source"`
}

type cacheEntry struct {
	works   []Work
	expires time.Time
}

// Client ходит в OpenAlex только при заданном mailto (вежливый пул); Crossref — фолбэк на любую
// ошибку или пустой ответ OpenAlex.
type Client struct {
	mailto      func(ctx context.Context) string
	apiKey      func(ctx context.Context) string
	enabled     func(ctx context.Context) bool
	recentYears func(ctx context.Context) int
	oa          *openalex.Client
	crBase      string
	http        *http.Client
	now         func() time.Time
	mu          sync.Mutex
	cache       map[string]cacheEntry
}

// New читает PUBSEARCH_MAILTO и PUBSEARCH_ENABLED на каждом вызове, чтобы переключатели админки
// работали без рестарта.
func New(ovr *runtimecfg.Overrides) *Client {
	c := newClient("", true, openalexBase, crossrefBase)
	cfg := openalex.FromEnv("rag-test-pubsearch/1.0")
	cfg.Attempts = 2
	c.oa = mustOpenAlex(cfg)
	c.mailto = func(ctx context.Context) string {
		return strings.TrimSpace(ovr.Get(ctx, "PUBSEARCH_MAILTO", ""))
	}
	c.apiKey = func(ctx context.Context) string {
		return strings.TrimSpace(ovr.Get(ctx, "OPENALEX_API_KEY", ""))
	}
	c.enabled = func(ctx context.Context) bool { return ovr.GetBool(ctx, "PUBSEARCH_ENABLED", true) }
	c.recentYears = func(ctx context.Context) int {
		n, err := strconv.Atoi(strings.TrimSpace(ovr.Get(ctx, "PUBSEARCH_RECENT_YEARS", strconv.Itoa(defaultRecentYears))))
		if err != nil || n < 0 {
			return defaultRecentYears
		}
		return n
	}
	return c
}

// mustOpenAlex не паникует: при кривом прокси пишет в журнал и ходит напрямую.
func mustOpenAlex(cfg openalex.Config) *openalex.Client {
	client, err := openalex.New(cfg)
	if err != nil {
		slog.Error("openalex proxy ignored", "error", err)
		cfg.Proxy = ""
		client, _ = openalex.New(cfg)
	}
	return client
}

func newClient(mailto string, enabled bool, oaBase, crBase string) *Client {
	m := strings.TrimSpace(mailto)
	return &Client{
		mailto:      func(context.Context) string { return m },
		apiKey:      func(context.Context) string { return "" },
		enabled:     func(context.Context) bool { return enabled },
		recentYears: func(context.Context) int { return 0 },
		oa:          mustOpenAlex(openalex.Config{BaseURL: oaBase, UserAgent: "rag-test-pubsearch/1.0", Attempts: 2}),
		crBase:      crBase,
		http:        &http.Client{},
		now:         time.Now,
		mu:          sync.Mutex{},
		cache:       map[string]cacheEntry{},
	}
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]Work, error) {
	query = strings.TrimSpace(query)
	if !c.enabled(ctx) || query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	ry := c.recentYears(ctx)
	key := strconv.Itoa(limit) + "|" + strconv.Itoa(ry) + "|" + query
	if works, ok := c.fromCache(key); ok {
		return works, nil
	}
	ctx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	works, oaErr := c.searchOpenAlex(ctx, query, limit, ry)
	if len(works) == 0 {
		crWorks, crErr := c.searchCrossref(ctx, query, limit, ry)
		if crErr != nil {
			return nil, errors.Join(oaErr, crErr)
		}
		works = crWorks
	}
	c.toCache(key, works)
	return works, nil
}

func (c *Client) fromDate(recentYears int) string {
	if recentYears <= 0 {
		return ""
	}
	return c.now().AddDate(-recentYears, 0, 0).Format("2006-01-02")
}

func (c *Client) fromCache(key string) ([]Work, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok || c.now().After(e.expires) {
		return nil, false
	}
	return e.works, true
}

func (c *Client) toCache(key string, works []Work) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cacheEntry{works: works, expires: c.now().Add(cacheTTL)}
}

func (c *Client) getJSON(ctx context.Context, endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, perSourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("pubsearch: build request: %w", err)
	}
	ua := "rag-test-pubsearch/1.0"
	if m := c.mailto(ctx); m != "" {
		ua += " (mailto:" + m + ")"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pubsearch: get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pubsearch: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("pubsearch: read body: %w", err)
	}
	return body, nil
}
