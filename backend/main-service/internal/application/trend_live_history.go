package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/example/main-service/internal/platform/openalex"
)

// Окно arXiv в 14 месяцев не видит возраста термина: без истории OpenAlex зрелые темы сошли бы за слабые сигналы.

type LiveTermHistory map[int]int

// liveHistoryParallel: темп и бюджет держит сам клиент OpenAlex, а кандидатов в пачке до сорока.
const liveHistoryParallel = 4

// TermHistories не кладёт в ответ неудавшуюся фразу: иначе сбой выглядел бы как «работ не было».
func TermHistories(ctx context.Context, client *openalex.Client, phrases []string,
) (map[string]LiveTermHistory, error) {
	out := make(map[string]LiveTermHistory, len(phrases))
	var errs []error
	var mu sync.Mutex
	var wg sync.WaitGroup
	slot := make(chan struct{}, liveHistoryParallel)
	for _, phrase := range phrases {
		slot <- struct{}{}
		wg.Go(func() {
			defer func() { <-slot }()
			history, err := liveTermHistory(ctx, client, phrase)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%q: %w", phrase, err))
				return
			}
			out[phrase] = history
		})
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

func liveTermHistory(ctx context.Context, client *openalex.Client, phrase string) (LiveTermHistory, error) {
	// Запятая разделяет фильтры OpenAlex, кавычка закрывает фразу; дефис и пробел OpenAlex не различает.
	clean := strings.Join(strings.FieldsFunc(phrase, func(r rune) bool { return r == ',' || r == '"' || r == '|' }), " ")
	raw, err := client.Get(ctx, "/works", url.Values{"filter": {`title_and_abstract.search:"` + clean + `"`},
		"group_by": {"publication_year"}, "per_page": {"200"}})
	if err != nil {
		return nil, fmt.Errorf("история термина: %w", err)
	}
	var answer struct {
		GroupBy []struct {
			Key   string `json:"key"`
			Count int    `json:"count"`
		} `json:"group_by"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("история термина: %w", err)
	}
	history := LiveTermHistory{}
	for _, group := range answer.GroupBy {
		if year, err := strconv.Atoi(group.Key); err == nil {
			history[year] = group.Count
		}
	}
	return history, nil
}

func liveHistoryWorks(h LiveTermHistory, until int) int {
	n := 0
	for year, count := range h {
		if year <= until {
			n += count
		}
	}
	return n
}

// liveHistoryInputs — семь входов модели от года среза без неполного текущего; nil — все неизмерены.
func liveHistoryInputs(h LiveTermHistory, month string) []float64 {
	year, err := strconv.Atoi(month[:min(4, len(month))])
	if h == nil || err != nil {
		return []float64{math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()}
	}
	total := float64(liveHistoryWorks(h, year-1))
	if total == 0 {
		return make([]float64, 7)
	}
	before := float64(liveHistoryWorks(h, year-7))
	mid := float64(liveHistoryWorks(h, year-3)-liveHistoryWorks(h, year-7)) / 4
	recent := float64(h[year-2]+h[year-1]) / 2
	age := 0.0
	for y := year - 30; y < year; y++ {
		if float64(h[y]) >= max(3, 0.02*total) {
			age = float64(year - y)
			break
		}
	}
	const span = 8
	xMean, yMean := float64(year)-4.5, 0.0
	for y := year - span; y < year; y++ {
		yMean += math.Log1p(float64(h[y]))
	}
	yMean /= span
	num, den := 0.0, 0.0
	for y := year - span; y < year; y++ {
		dx := float64(y) - xMean
		num += dx * (math.Log1p(float64(h[y])) - yMean)
		den += dx * dx
	}
	return []float64{math.Log1p(total), before / total, math.Log(recent+1) - math.Log(mid+1), math.Log1p(recent), age,
		num / den, float64(h[year-3]+h[year-2]+h[year-1]) / total}
}

// liveHistoryCache помнит историю до конца месяца: прошлые годы не меняются.
// Отказ бесплатного лимита переспрашивается ключом: история — самая весомая группа входов.
type liveHistoryCache struct {
	clients []*openalex.Client
	mu      sync.Mutex
	known   map[string]LiveTermHistory
	store   liveStore
	loaded  map[string]bool
}

func newLiveHistoryCache(clients ...*openalex.Client) *liveHistoryCache {
	return &liveHistoryCache{clients: slices.DeleteFunc(clients, func(c *openalex.Client) bool { return c == nil }),
		known: map[string]LiveTermHistory{}}
}

// histories глотает сбой OpenAlex: без истории модель считает по одному окну arXiv.
func (c *liveHistoryCache) histories(ctx context.Context, phrases []string, month string) map[string]LiveTermHistory {
	out := make(map[string]LiveTermHistory, len(phrases))
	missing := []string{}
	c.load(ctx, month)
	c.mu.Lock()
	for _, phrase := range phrases {
		if h, ok := c.known[month+"|"+phrase]; ok {
			out[phrase] = h
		} else if phrase != "" && !slices.Contains(missing, phrase) {
			missing = append(missing, phrase)
		}
	}
	c.mu.Unlock()
	for _, client := range c.clients {
		if len(missing) == 0 {
			break
		}
		// Замер 27.09: пересчёт сотни направлений съел суточный бюджет к утру, и живой поиск жюри остался бы без
		// истории терминов — главного признака модели. Фон не трогает долю, оставленную поискам людей.
		if liveBackground(ctx) && liveScholarReserved(client) != nil {
			continue
		}
		fresh, err := TermHistories(ctx, client, missing)
		if err != nil {
			slog.Warn("radar term history", "phrases", len(missing), "answered", len(fresh), "error", err)
		}
		c.mu.Lock()
		for phrase, h := range fresh {
			c.known[month+"|"+phrase] = h
			out[phrase] = h
		}
		c.mu.Unlock()
		missing = slices.DeleteFunc(missing, func(p string) bool { _, ok := fresh[p]; return ok })
		if len(fresh) > 0 {
			c.save(ctx, month)
		}
	}
	return out
}
