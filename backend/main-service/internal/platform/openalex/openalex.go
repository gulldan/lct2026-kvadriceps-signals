// Package openalex — единый клиент OpenAlex, чтобы все сервисы жили по одному бюджету: ключ даёт 1 $
// в день (10 000 списочных запросов) против 1 000 без ключа, сброс в полночь UTC, а 429 несёт секунды
// до сброса.
package openalex

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/jsonx"
)

// MaxPerPage — потолок страницы OpenAlex и заодно предел значений через «|» в одном фильтре.
const (
	DefaultBaseURL = "https://api.openalex.org"
	MaxPerPage     = 100
	pace           = 120 * time.Millisecond
	attempts       = 6
	giveUpAfter    = 5 * time.Minute
	longestWait    = 90 * time.Second
	maxBody        = 32 << 20
)

type Config struct {
	BaseURL   string
	APIKey    string
	Mailto    string
	Proxy     string
	UserAgent string
	Timeout   time.Duration
	// Ноль — шесть повторов на 429 и 5xx, под ночной цикл; интерактивному поиску хватит двух.
	Attempts int
}

func FromEnv(userAgent string) Config {
	return Config{BaseURL: config.Get("OPENALEX_API_URL", DefaultBaseURL),
		APIKey: strings.TrimSpace(os.Getenv("OPENALEX_API_KEY")), Mailto: strings.TrimSpace(os.Getenv("OPENALEX_MAILTO")),
		Proxy: strings.TrimSpace(os.Getenv("OPENALEX_PROXY_URL")), UserAgent: userAgent}
}

type Client struct {
	cfg    Config
	HTTP   *http.Client
	Sleep  func(context.Context, time.Duration) error
	mu     *sync.Mutex
	nextAt *time.Time
	// budgets общий для всех копий из With: провайдер считает день по ключу (без ключа — по адресу),
	// а не по значению клиента.
	budgets *budgetBook
}

// Budget взят из последних заголовков X-RateLimit, поэтому учитывает траты других процессов
// с тем же ключом.
type Budget struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
	SeenAt    time.Time `json:"seen_at"`
}

type budgetBook struct {
	mu   sync.Mutex
	byID map[string]Budget
}

func (c *Client) Budget() (Budget, bool) {
	if c.budgets == nil {
		return Budget{}, false
	}
	c.budgets.mu.Lock()
	defer c.budgets.mu.Unlock()
	budget, ok := c.budgets.byID[c.cfg.APIKey]
	return budget, ok && time.Now().Before(budget.ResetAt)
}

// Probe обновляет бюджет бесплатным запросом: чтение одной работы по id не тратит кредиты,
// но возвращает заголовки.
func (c *Client) Probe(ctx context.Context) (Budget, bool) {
	_, _ = c.Get(ctx, "/works/W2741809807", url.Values{"select": {"id"}})
	return c.Budget()
}

func (c *Client) note(header http.Header, spentFor time.Duration) {
	if c.budgets == nil {
		return
	}
	limit, errLimit := strconv.Atoi(header.Get("X-Ratelimit-Limit"))
	remaining, errRemaining := strconv.Atoi(header.Get("X-Ratelimit-Remaining"))
	reset, errReset := strconv.Atoi(header.Get("X-Ratelimit-Reset"))
	now := time.Now()
	c.budgets.mu.Lock()
	defer c.budgets.mu.Unlock()
	switch {
	case errLimit == nil && errRemaining == nil && errReset == nil:
		c.budgets.byID[c.cfg.APIKey] = Budget{Limit: limit, Remaining: remaining,
			ResetAt: now.Add(time.Duration(reset) * time.Second), SeenAt: now}
	case spentFor > 0:
		budget := c.budgets.byID[c.cfg.APIKey]
		budget.Remaining, budget.ResetAt, budget.SeenAt = 0, now.Add(spentFor), now
		c.budgets.byID[c.cfg.APIKey] = budget
	}
}

// New держит запросы на HTTP/1.1: HTTP/2 у провайдера зависал в интеграционных проверках.
func New(cfg Config) (*Client, error) {
	transport := new(http.Transport)
	if defaults, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaults.Clone()
	}
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = new(tls.Config)
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	if cfg.Proxy != "" {
		proxy, err := url.Parse(cfg.Proxy)
		if err != nil || proxy.Host == "" || !slices.Contains([]string{"http", "https", "socks5", "socks5h"}, proxy.Scheme) {
			return nil, errors.New("openalex: OPENALEX_PROXY_URL must be an http, https or socks5 URL with a host")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &Client{cfg: cfg, HTTP: &http.Client{Timeout: cmp.Or(cfg.Timeout, 30*time.Second), Transport: transport},
		Sleep: sleep, mu: new(sync.Mutex), nextAt: new(time.Time), budgets: &budgetBook{byID: map[string]Budget{}}}, nil
}

func (c *Client) With(apiKey, mailto string) *Client {
	other := *c
	other.cfg.APIKey, other.cfg.Mailto = strings.TrimSpace(apiKey), strings.TrimSpace(mailto)
	return &other
}

func (c *Client) Configured() bool { return c.cfg.APIKey != "" || c.cfg.Mailto != "" }

func (c *Client) Keyed() bool { return c.cfg.APIKey != "" }

var ErrNotFound = errors.New("openalex: not found")

// BudgetError — 429 со сбросом дальше giveUpAfter: день исчерпан, повторы лишь сожгут время цикла.
type BudgetError struct{ ResetIn time.Duration }

func (e *BudgetError) Error() string {
	return "openalex: daily request budget exhausted, resets in " + e.ResetIn.Round(time.Minute).String()
}

type StatusError struct{ Status int }

func (e *StatusError) Error() string { return fmt.Sprintf("openalex: status %d", e.Status) }

func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	values := url.Values{}
	if query != nil {
		values = query.Clone()
	}
	if c.cfg.APIKey != "" {
		values.Set("api_key", c.cfg.APIKey)
	}
	if c.cfg.Mailto != "" {
		values.Set("mailto", c.cfg.Mailto)
	}
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + path
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}
	for attempt := range cmp.Or(c.cfg.Attempts, attempts) {
		if err := c.pace(ctx); err != nil {
			return nil, err
		}
		body, header, status, err := c.do(ctx, endpoint)
		if header != nil {
			c.note(header, 0)
		}
		switch {
		case err != nil:
			return nil, err
		case status == http.StatusOK:
			return body, nil
		case status == http.StatusNotFound:
			return nil, ErrNotFound
		case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
			wait := retryAfter(header, time.Duration(1<<attempt)*time.Second)
			if wait > giveUpAfter {
				c.note(http.Header{}, wait)
				return nil, &BudgetError{ResetIn: wait}
			}
			if err = c.Sleep(ctx, min(wait, longestWait)); err != nil {
				return nil, err
			}
		default:
			return nil, &StatusError{Status: status}
		}
	}
	return nil, errors.New("openalex: retries exhausted")
}

func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	wait := time.Until(*c.nextAt)
	*c.nextAt = time.Now().Add(max(wait, 0) + pace)
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	return c.Sleep(ctx, wait)
}

func (c *Client) do(ctx context.Context, endpoint string) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("openalex: build request: %w", err)
	}
	req.Header.Set("User-Agent", cmp.Or(c.cfg.UserAgent, "rag-test/1.0"))
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("openalex: get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("openalex: read: %w", err)
	}
	if len(body) > maxBody {
		return nil, resp.Header, resp.StatusCode, errors.New("openalex: response exceeds 32 MiB")
	}
	return body, resp.Header, resp.StatusCode, nil
}

func retryAfter(header http.Header, fallback time.Duration) time.Duration {
	for _, name := range []string{"Retry-After", "X-RateLimit-Reset"} {
		if seconds, err := strconv.Atoi(header.Get(name)); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("openalex: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

type Entity struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Authorship struct {
	Author       Entity   `json:"author"`
	Institutions []Entity `json:"institutions"`
}

type Location struct {
	Source         *Entity `json:"source"`
	LandingPageURL string  `json:"landing_page_url"`
}

type Work struct {
	ID                    string           `json:"id"`
	DOI                   string           `json:"doi"`
	Title                 string           `json:"title"`
	Type                  string           `json:"type"`
	Language              string           `json:"language"`
	PublicationYear       int              `json:"publication_year"`
	PublicationDate       string           `json:"publication_date"`
	CitedByCount          int              `json:"cited_by_count"`
	PrimaryLocation       *Location        `json:"primary_location"`
	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
	Authorships           []Authorship     `json:"authorships"`
}

type Page struct {
	Meta struct {
		Count   int `json:"count"`
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
	} `json:"meta"`
	Results []Work `json:"results"`
}

func (c *Client) Works(ctx context.Context, query url.Values) (Page, error) {
	var page Page
	body, err := c.Get(ctx, "/works", query)
	if err != nil {
		return page, err
	}
	if err = jsonx.Unmarshal(body, &page); err != nil {
		return page, fmt.Errorf("openalex: decode works: %w", err)
	}
	return page, nil
}

// WorkByDOI отдаёт и сырые байты, чтобы вызывающий хэшировал ровно ответ провайдера.
func (c *Client) WorkByDOI(ctx context.Context, doi string, fields ...string) (Work, []byte, error) {
	var work Work
	query := url.Values{}
	if len(fields) > 0 {
		query.Set("select", strings.Join(fields, ","))
	}
	body, err := c.Get(ctx, "/works/doi:"+url.PathEscape(PlainDOI(doi)), query)
	if err != nil {
		return work, nil, err
	}
	if err = jsonx.Unmarshal(body, &work); err != nil {
		return work, nil, fmt.Errorf("openalex: decode work: %w", err)
	}
	return work, body, nil
}

func PlainDOI(doi string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(doi)), "https://doi.org/"), "doi:")
}

func (w Work) PlainDOI() string { return PlainDOI(w.DOI) }

func (w Work) Venue() string {
	if w.PrimaryLocation == nil || w.PrimaryLocation.Source == nil {
		return ""
	}
	return w.PrimaryLocation.Source.DisplayName
}

func (w Work) Abstract() string {
	if len(w.AbstractInvertedIndex) == 0 {
		return ""
	}
	type placed struct {
		position int
		word     string
	}
	words := []placed{}
	for word, positions := range w.AbstractInvertedIndex {
		for _, position := range positions {
			words = append(words, placed{position, word})
		}
	}
	slices.SortFunc(words, func(a, b placed) int { return cmp.Or(cmp.Compare(a.position, b.position), cmp.Compare(a.word, b.word)) })
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = word.word
	}
	return strings.Join(out, " ")
}
