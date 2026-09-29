package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/aws/smithy-go"
	"github.com/example/main-service/internal/platform/openalex"
)

// liveScholarReserve — процент суточного бюджета OpenAlex, который журнальный канал оставляет истории терминов
// модели.
const liveScholarReserve = 30

// openalex_budget — журналы не опрошены, остаток оставлен модели; openalex_exhausted — кончился и он.
const (
	liveLimitOpenAlexBudget    = "openalex_budget"
	liveLimitOpenAlexExhausted = "openalex_exhausted"
)

type liveBackgroundKey struct{}

// Фоновая работа — пересчёт направлений и ночная пересборка ответов: её никто не ждёт.
func liveInBackground(ctx context.Context) context.Context {
	return context.WithValue(ctx, liveBackgroundKey{}, true)
}

func liveBackground(ctx context.Context) bool {
	background, _ := ctx.Value(liveBackgroundKey{}).(bool)
	return background
}

// liveScholarReserved отвечает той же ошибкой, что исчерпанный бюджет: поиск объясняет её строкой шапки.
func liveScholarReserved(oa *openalex.Client) error {
	budget, ok := oa.Budget()
	if !ok || budget.Remaining > 0 && budget.Remaining*100 >= budget.Limit*liveScholarReserve {
		return nil
	}
	return &openalex.BudgetError{ResetIn: time.Until(budget.ResetAt)}
}

func liveScholarFailure(err error) LiveLimit {
	if budget, ok := errors.AsType[*openalex.BudgetError](err); ok {
		return liveLimitOf(liveLimitOpenAlexBudget, "scholar: "+budget.Error(),
			map[string]int{"reset_hours": int(budget.ResetIn.Hours())})
	}
	return liveLimitFail(liveLimitScholarOff, "scholar", err)
}

// historySpent не считает неизвестный бюджет исчерпанным.
func (s *TrendLiveService) historySpent() bool {
	if s.oa == nil {
		return false
	}
	for _, client := range []*openalex.Client{s.oa.With("", ""), s.oa} {
		if budget, ok := client.Budget(); !ok || budget.Remaining > 0 {
			return false
		}
	}
	return true
}

func (s *TrendLiveService) liveMarkHistoryBudget(limits []LiveLimit) []LiveLimit {
	spent := s.historySpent()
	limits = slices.DeleteFunc(slices.Clone(limits), func(limit LiveLimit) bool {
		return limit.Code == liveLimitOpenAlexExhausted || spent && limit.Code == liveLimitOpenAlexBudget
	})
	if spent {
		limits = append(limits, liveLimitOf(liveLimitOpenAlexExhausted,
			"openalex: daily budget spent, term history unavailable", nil))
	}
	return limits
}

type LiveOpenAlexBudget struct {
	// Credential: key — ключ API, address — бесплатный лимит адреса стенда.
	Credential string    `json:"credential"`
	Known      bool      `json:"known"`
	Limit      int       `json:"limit,omitempty"`
	Remaining  int       `json:"remaining,omitempty"`
	Spent      int       `json:"spent,omitempty"`
	Reserve    int       `json:"reserve,omitempty"`
	ResetAt    time.Time `json:"reset_at,omitzero"`
	SeenAt     time.Time `json:"seen_at,omitzero"`
}

// OpenAlexBudgets уточняет неизвестный бюджет бесплатным запросом: чтение одной работы кредитов не стоит.
func (s *TrendLiveService) OpenAlexBudgets(ctx context.Context) []LiveOpenAlexBudget {
	if s.oa == nil {
		return []LiveOpenAlexBudget{}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	clients := []*openalex.Client{s.oa.With("", "")}
	if s.oa.Keyed() {
		clients = append(clients, s.oa)
	}
	out := make([]LiveOpenAlexBudget, 0, len(clients))
	for i, client := range clients {
		budget, ok := client.Budget()
		if !ok {
			budget, ok = client.Probe(ctx)
		}
		entry := LiveOpenAlexBudget{Credential: "address", Known: ok}
		if i > 0 {
			entry.Credential = "key"
		}
		if ok {
			entry.Limit, entry.Remaining, entry.Spent = budget.Limit, budget.Remaining, budget.Limit-budget.Remaining
			entry.Reserve, entry.ResetAt, entry.SeenAt = budget.Limit*liveScholarReserve/100, budget.ResetAt, budget.SeenAt
		}
		out = append(out, entry)
	}
	return out
}

// keepTermHistory хранит историю терминов: прошлые годы не меняются, и после выкладки их незачем покупать заново.
func (s *TrendLiveService) keepTermHistory() {
	if s.store == nil {
		return
	}
	history := s.termHistory()
	history.mu.Lock()
	history.store = s.store
	history.mu.Unlock()
}

// liveHistoryObject задаёт срок хранения ключом: следующий месяц пишет новый объект.
func liveHistoryObject(month string) string {
	return "trends/live/openalex/history-" + month + ".json"
}

// load не блокирует месяц при сбое чтения: без сохранённой истории модель спросит OpenAlex.
func (c *liveHistoryCache) load(ctx context.Context, month string) {
	c.mu.Lock()
	store, done := c.store, c.loaded[month]
	c.mu.Unlock()
	if store == nil || done {
		return
	}
	raw, err := store.GetBytes(ctx, liveHistoryObject(month))
	if err != nil && !liveHistoryMissing(err) {
		slog.Warn("radar term history cache not read", "month", month, "error", err)
		return
	}
	stored := map[string]LiveTermHistory{}
	if err == nil {
		if err := json.Unmarshal(raw, &stored); err != nil {
			slog.Warn("radar term history cache unreadable", "month", month, "error", err)
			return
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded == nil {
		c.loaded = map[string]bool{}
	}
	c.loaded[month] = true
	for phrase, history := range stored {
		if _, ok := c.known[month+"|"+phrase]; !ok {
			c.known[month+"|"+phrase] = history
		}
	}
}

// Подтверждённое отсутствие объекта — пустой кэш; отказ доступа или транспорта требует повторного чтения.
func liveHistoryMissing(err error) bool {
	apiErr, ok := errors.AsType[smithy.APIError](err)
	return ok && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound")
}

// save пишет месяц целиком: одновременные записи могут потерять часть фраз — это кредит при следующем
// вопросе, а не ошибка.
func (c *liveHistoryCache) save(ctx context.Context, month string) {
	c.mu.Lock()
	store := c.store
	// Частичная память после сбоя чтения не должна затирать сохранённый месяц.
	if store == nil || !c.loaded[month] {
		c.mu.Unlock()
		return
	}
	entries := map[string]LiveTermHistory{}
	for key, history := range c.known {
		if phrase, ok := strings.CutPrefix(key, month+"|"); ok {
			entries[phrase] = history
		}
	}
	c.mu.Unlock()
	raw, err := json.Marshal(entries)
	if err == nil {
		err = store.Put(context.WithoutCancel(ctx), liveHistoryObject(month), bytes.NewReader(raw), int64(len(raw)),
			"application/json")
	}
	if err != nil {
		slog.Warn("radar term history cache not saved", "month", month, "error", err)
	}
}
