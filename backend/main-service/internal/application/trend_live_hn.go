package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Окна и формулы повторяют исследование (hn.py, eval3.py): на них обучены веса модели.

// LiveHNCounts — упоминания точной фразы в окнах до Y−6, Y−6…Y−3 и Y−2…Y−1, где Y — год среза.
type LiveHNCounts struct {
	Before      int `json:"before"`
	Mid         int `json:"mid"`
	Recent      int `json:"recent"`
	BeforeStory int `json:"before_story"`
	MidStory    int `json:"mid_story"`
	RecentStory int `json:"recent_story"`
}

const (
	// Algolia без ключа держит около десяти тысяч запросов в час с адреса, а фраза стоит шесть.
	liveHNParallel = 6
	liveHNTimeout  = 30 * time.Second
)

// HackerNewsCounts не отдаёт фразу с неудавшимся запросом: нули вместо сбоя выглядели бы как молчание.
func HackerNewsCounts(ctx context.Context, base string, phrases []string, month string,
) (map[string]*LiveHNCounts, error) {
	year, err := strconv.Atoi(month[:min(4, len(month))])
	if err != nil {
		return nil, fmt.Errorf("hn: month %q: %w", month, err)
	}
	jan := func(y int) int64 { return time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC).Unix() }
	// Порядок как в исследовании: окно, затем то же окно только по историям.
	windows := [3][2]int64{{0, jan(year - 6)}, {jan(year - 6), jan(year - 2)}, {jan(year - 2), jan(year)}}
	src := &liveSources{http: &http.Client{Timeout: liveHNTimeout}}
	endpoint := cmp.Or(base, liveHNBase) + "/search"
	values := make([][6]int, len(phrases))
	fails := make([][6]error, len(phrases))
	var wg sync.WaitGroup
	slot := make(chan struct{}, liveHNParallel)
	for p, phrase := range phrases {
		for i := range 6 {
			slot <- struct{}{}
			wg.Go(func() {
				defer func() { <-slot }()
				values[p][i], fails[p][i] = liveHNHits(ctx, src, endpoint, phrase, windows[i/2], i%2 == 1)
			})
		}
	}
	wg.Wait()
	out := make(map[string]*LiveHNCounts, len(phrases))
	var errs []error
	for p, phrase := range phrases {
		if err := errors.Join(fails[p][:]...); err != nil {
			errs = append(errs, fmt.Errorf("%q: %w", phrase, err))
			continue
		}
		v := values[p]
		out[phrase] = &LiveHNCounts{Before: v[0], BeforeStory: v[1], Mid: v[2], MidStory: v[3], Recent: v[4], RecentStory: v[5]}
	}
	return out, errors.Join(errs...)
}

func liveHNHits(ctx context.Context, src *liveSources, endpoint, phrase string, window [2]int64, story bool) (int, error) {
	clean := strings.Join(strings.Fields(strings.ReplaceAll(phrase, `"`, " ")), " ")
	values := url.Values{"query": {`"` + clean + `"`}, "hitsPerPage": {"0"},
		"numericFilters": {fmt.Sprintf("created_at_i>=%d,created_at_i<%d", window[0], window[1])}}
	if story {
		values.Set("tags", "story")
	}
	body, _, status, err := src.get(ctx, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("hn: %w", err)
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("hn: status %d", status)
	}
	var answer struct {
		Hits *int `json:"nbHits"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return 0, fmt.Errorf("hn: parse: %w", err)
	}
	if answer.Hits == nil {
		return 0, errors.New("hn: answer without nbHits")
	}
	return *answer.Hits, nil
}

// LiveHNInputs — шесть входов модели в порядке обучения; nil — Hacker News не ответил, все неизмерены.
func LiveHNInputs(c *LiveHNCounts) []float64 {
	if c == nil {
		return []float64{math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()}
	}
	total := float64(c.Before + c.Mid + c.Recent)
	share := 0.0
	if total > 0 {
		share = float64(c.Before) / total
	}
	growth := func(recent, mid int) float64 { return math.Log(float64(recent)/2+1) - math.Log(float64(mid)/4+1) }
	return []float64{math.Log1p(total), share, growth(c.Recent, c.Mid), math.Log1p(float64(c.Recent)),
		math.Log1p(float64(c.RecentStory)), growth(c.RecentStory, c.MidStory)}
}

// liveHNCache помнит упоминания до конца месяца: окна кончаются прошлым годом и не меняются.
type liveHNCache struct {
	base  string
	mu    sync.Mutex
	known map[string]*LiveHNCounts
}

func newLiveHNCache(base string) *liveHNCache {
	return &liveHNCache{base: base, known: map[string]*LiveHNCounts{}}
}

// Сбой только пишется в журнал: без Hacker News модель считает по остальным входам.
func (c *liveHNCache) counts(ctx context.Context, phrases []string, month string) map[string]*LiveHNCounts {
	out := make(map[string]*LiveHNCounts, len(phrases))
	missing := []string{}
	c.mu.Lock()
	for _, phrase := range phrases {
		if counts, ok := c.known[month+"|"+phrase]; ok {
			out[phrase] = counts
		} else if phrase != "" && !slices.Contains(missing, phrase) {
			missing = append(missing, phrase)
		}
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out
	}
	fresh, err := HackerNewsCounts(ctx, c.base, missing, month)
	if err != nil {
		slog.Warn("radar hacker news counts", "phrases", len(missing), "answered", len(fresh), "error", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for phrase, counts := range fresh {
		c.known[month+"|"+phrase] = counts
		out[phrase] = counts
	}
	return out
}
