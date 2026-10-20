package application

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Слой ранних денег: раунды pre-seed, seed и Series A, гранты стартапам и отборы акселераторов за полгода.
// Он объясняет и дополняет сигнал, но во вход модели этапа 1 не идёт: эталон сам рыночный (замер 18.10).

const (
	liveMoneyKey     = "trends/live/money/early-money-v1.json.gz"
	liveMoneyMonths  = 6
	liveMoneyRefresh = 24 * time.Hour
	liveMoneyRetry   = time.Hour
	liveMoneyBudget  = 20 * time.Minute
	liveMoneyLatest  = 12
	liveMoneyMatched = 12
)

// LiveMoneyEvent без строки реестра или дословной цитаты Quote в слой не попадает.
type LiveMoneyEvent struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Stage   string `json:"stage"`
	Company string `json:"company,omitempty"`
	// Region — макрорегион страны компании; без страны — регион источника, у глобального набора «unknown».
	Region      string `json:"region"`
	Country     string `json:"country,omitempty"`
	Date        string `json:"date"`
	Amount      string `json:"amount,omitempty"`
	Investors   string `json:"investors,omitempty"`
	Rubric      string `json:"rubric"`
	RubricBasis string `json:"rubric_basis,omitempty"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Source      string `json:"source"`
	License     string `json:"license"`
	Quote       string `json:"quote,omitempty"`
	// Text хранится для сверки с технологией и наружу не отдаётся.
	Text string `json:"text,omitempty"`
	// AmountValue — сумма полным числом в валюте Currency (ISO 4217): экран пишет её в формате языка, а Amount
	// остаётся исходным текстом. Без валюты в тексте Currency пуст, и экран показывает Amount как есть.
	AmountValue float64 `json:"amount_value,omitzero"`
	Currency    string  `json:"currency,omitempty"`
}

// LiveMoneySource — что дал источник за проход; отказ записывается, а не превращается в ноль сделок.
type LiveMoneySource struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Region  string `json:"region"`
	License string `json:"license"`
	URL     string `json:"url"`
	Records int    `json:"records"`
	Events  int    `json:"events"`
	Error   string `json:"error,omitempty"`
}

type LiveMoneyBoard struct {
	BuiltAt time.Time         `json:"built_at"`
	From    string            `json:"from"`
	Through string            `json:"through"`
	Method  string            `json:"method"`
	Events  []LiveMoneyEvent  `json:"events"`
	Sources []LiveMoneySource `json:"sources"`
}

// LiveMoneyCell — число событий одной рубрики, региона, вида и месяца; фильтры экран применяет сам.
type LiveMoneyCell struct {
	Rubric string `json:"rubric"`
	Region string `json:"region"`
	Kind   string `json:"kind"`
	Stage  string `json:"stage"`
	Month  string `json:"month"`
	Count  int    `json:"count"`
}

// LiveMoneyScience — рост научного задела рубрики по направлениям радара за два полугодия.
type LiveMoneyScience struct {
	Rubric           string `json:"rubric"`
	Topics           int    `json:"topics"`
	Prior            int    `json:"prior"`
	Recent           int    `json:"recent"`
	PriorPopulation  int    `json:"prior_population"`
	RecentPopulation int    `json:"recent_population"`
}

type LiveMoneySummary struct {
	Status  string             `json:"status"`
	BuiltAt string             `json:"built_at,omitempty"`
	From    string             `json:"from,omitempty"`
	Through string             `json:"through,omitempty"`
	Method  string             `json:"method,omitempty"`
	Months  []string           `json:"months"`
	Cells   []LiveMoneyCell    `json:"cells"`
	Science []LiveMoneyScience `json:"science"`
	// Окна роста науки («2025-10–2026-03»): экран подписывает ими столбец «Наука» рядом с окнами денег.
	SciencePrior  string            `json:"science_prior,omitempty"`
	ScienceRecent string            `json:"science_recent,omitempty"`
	Latest        []LiveMoneyEvent  `json:"latest"`
	Sources       []LiveMoneySource `json:"sources"`
	Total         int               `json:"total"`
	Unsorted      int               `json:"unsorted"`
}

type LiveMoneyMatch struct {
	Status string           `json:"status"`
	Total  int              `json:"total"`
	Kinds  map[string]int   `json:"kinds"`
	Events []LiveMoneyEvent `json:"events"`
}

type liveMoneyIndex struct {
	mu         sync.Mutex
	board      *LiveMoneyBoard
	read       bool
	refreshing bool
	failedAt   time.Time
}

func (s *TrendLiveService) keepMoney(ctx context.Context) {
	index := &s.eventData.money
	index.mu.Lock()
	defer index.mu.Unlock()
	if !index.read {
		index.read = true
		index.board = s.readMoney(ctx)
	}
	now := s.now()
	stale := index.board == nil || now.Sub(index.board.BuiltAt) > liveMoneyRefresh ||
		now.Sub(index.board.BuiltAt) > liveMoneyRetry && !liveMoneyFeedsSeen(index.board)
	if stale && !index.refreshing && now.Sub(index.failedAt) > liveMoneyRetry {
		index.refreshing = true
		go s.refreshMoney(ctx)
	}
}

// liveMoneyFeedsSeen ложен, пока сборщик не обошёл каждую настроенную ленту: суточная пересборка ждала бы
// новые ленты до завтра. Лента, ответившая ошибкой, считается обойдённой — иначе мёртвая лента гоняла бы
// пересборку каждый час.
func liveMoneyFeedsSeen(board *LiveMoneyBoard) bool {
	for _, feed := range liveMoneyFeeds() {
		seen := slices.ContainsFunc(board.Sources, func(source LiveMoneySource) bool {
			return source.Name == feed.name && (source.Records > 0 || source.Error != "")
		})
		if !seen {
			return false
		}
	}
	return true
}

func (s *TrendLiveService) readMoney(ctx context.Context) *LiveMoneyBoard {
	raw, err := s.store.GetBytes(ctx, liveMoneyKey)
	if err != nil {
		return nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	defer func() { _ = reader.Close() }()
	var board LiveMoneyBoard
	if json.UnmarshalRead(reader, &board) != nil {
		return nil
	}
	// Доска, собранная до чистки, правится при чтении: иначе мусор в именах и суммах дожил бы до суточной пересборки.
	board.Events = liveMoneyTidy(board.Events)
	return &board
}

// refreshMoney держит прежнюю доску до конца сборки новой: вчерашние сделки лучше пустого экрана.
func (s *TrendLiveService) refreshMoney(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, liveMoneyBudget)
	defer cancel()
	board := s.buildMoney(ctx)
	err := s.saveMoney(ctx, board)
	index := &s.eventData.money
	index.mu.Lock()
	defer index.mu.Unlock()
	index.refreshing = false
	if len(board.Events) > 0 {
		index.board = board
	}
	if err != nil || len(board.Events) == 0 {
		index.failedAt = s.now()
		slog.Warn("early money refresh failed", "events", len(board.Events), "error", err)
		return
	}
	slog.Info("early money refreshed", "events", len(board.Events), "sources", len(board.Sources))
}

func (s *TrendLiveService) saveMoney(ctx context.Context, board *LiveMoneyBoard) error {
	if len(board.Events) == 0 {
		return errors.New("early money: ни один источник не дал событий")
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if err := json.MarshalWrite(writer, board); err != nil {
		return fmt.Errorf("early money: encode: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("early money: compress: %w", err)
	}
	if err := s.store.Put(ctx, liveMoneyKey, bytes.NewReader(buf.Bytes()), int64(buf.Len()), "application/gzip"); err != nil {
		return fmt.Errorf("early money: persist: %w", err)
	}
	return nil
}

// liveMoneyWindow — шесть полных месяцев до текущего и текущий неполный: свежие сделки важнее ровного ряда.
func liveMoneyWindow(now time.Time) (string, string, []string) {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -liveMoneyMonths, 0)
	months := make([]string, 0, liveMoneyMonths+1)
	for month := first; !month.After(now); month = month.AddDate(0, 1, 0) {
		months = append(months, month.Format("2006-01"))
	}
	return first.Format(time.DateOnly), now.Format(time.DateOnly), months
}

func (s *TrendLiveService) moneyBoard() (*LiveMoneyBoard, string) {
	index := &s.eventData.money
	index.mu.Lock()
	defer index.mu.Unlock()
	switch {
	case index.board != nil:
		return index.board, LiveSearchReady
	case index.refreshing || !index.read:
		return nil, "loading"
	}
	return nil, LiveSearchFailed
}

// MoneySummary считает выбранные события, сохраняя общий паспорт источников и научную базу.
func (s *TrendLiveService) MoneySummary(region, kind string) LiveMoneySummary {
	board, status := s.moneyBoard()
	summary := LiveMoneySummary{Status: status, Months: []string{}, Cells: []LiveMoneyCell{},
		Latest: []LiveMoneyEvent{}, Sources: []LiveMoneySource{}}
	summary.Science, summary.SciencePrior, summary.ScienceRecent = s.moneyScience()
	if board == nil {
		return summary
	}
	_, _, summary.Months = liveMoneyWindow(board.BuiltAt)
	summary.BuiltAt, summary.From, summary.Through = board.BuiltAt.Format(time.RFC3339), board.From, board.Through
	summary.Method, summary.Sources = board.Method, board.Sources
	cells := map[LiveMoneyCell]int{}
	for _, event := range board.Events {
		if region != "" && event.Region != region || kind != "" && event.Kind != kind {
			continue
		}
		summary.Total++
		if event.Rubric == "" {
			summary.Unsorted++
		}
		if event.Rubric != "" && len(summary.Latest) < liveMoneyLatest {
			summary.Latest = append(summary.Latest, liveMoneyPublic(event))
		}
		key := LiveMoneyCell{Rubric: event.Rubric, Region: event.Region, Kind: event.Kind, Stage: event.Stage,
			Month: event.Date[:min(len(event.Date), 7)]}
		cells[key]++
	}
	for cell, count := range cells {
		cell.Count = count
		summary.Cells = append(summary.Cells, cell)
	}
	slices.SortFunc(summary.Cells, func(a, b LiveMoneyCell) int {
		return cmp.Or(strings.Compare(a.Month, b.Month), strings.Compare(a.Rubric, b.Rubric),
			strings.Compare(a.Region, b.Region), strings.Compare(a.Kind, b.Kind), strings.Compare(a.Stage, b.Stage))
	})
	return summary
}

// MoneyMatch ищет технологию в тексте сделки целым началом слова: «battery» находит «battery-free», не «abattery».
func (s *TrendLiveService) MoneyMatch(terms []string) LiveMoneyMatch {
	board, status := s.moneyBoard()
	match := LiveMoneyMatch{Status: status, Kinds: map[string]int{}, Events: []LiveMoneyEvent{}}
	probes := liveMoneyProbes(terms)
	if board == nil || len(probes) == 0 {
		return match
	}
	for _, event := range board.Events {
		haystack := " " + strings.ToLower(event.Title+" "+event.Text+" "+event.Company) + " "
		if !slices.ContainsFunc(probes, func(probe string) bool { return liveWordPrefix(haystack, probe) }) {
			continue
		}
		match.Total++
		match.Kinds[event.Kind]++
		if len(match.Events) < liveMoneyMatched {
			match.Events = append(match.Events, liveMoneyPublic(event))
		}
	}
	return match
}

func liveWordPrefix(haystack, probe string) bool {
	for offset := 0; ; {
		i := strings.Index(haystack[offset:], probe)
		if i < 0 {
			return false
		}
		i += offset
		if r, _ := utf8.DecodeLastRuneInString(haystack[:i]); !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
		offset = i + len(probe)
	}
}

func liveMoneyPublic(event LiveMoneyEvent) LiveMoneyEvent {
	event.Text = ""
	return event
}

// moneyScience считает направления радара по рубрикам тем же правилом, что и сделки; окна роста у
// направлений одного снимка общие, поэтому их отдаёт любое посчитанное.
func (s *TrendLiveService) moneyScience() (out []LiveMoneyScience, prior, recent string) {
	s.mu.RLock()
	topics := []LiveTopic{}
	if s.snapshot != nil {
		topics = slices.Concat(s.snapshot.Topics, s.snapshot.Discovered)
	}
	s.mu.RUnlock()
	rubrics := liveRubrics()
	byRubric := map[string]*LiveMoneyScience{}
	for i := range topics {
		topic := &topics[i]
		rubric := liveRubricOf(rubrics, topic.Name+" "+strings.Join(topic.Query.Terms, " ")+" "+topic.Label)
		if rubric == "" || topic.Growth.Prior.Population == 0 || topic.Growth.Recent.Population == 0 {
			continue
		}
		science := byRubric[rubric]
		if science == nil {
			science = &LiveMoneyScience{Rubric: rubric, PriorPopulation: topic.Growth.Prior.Population,
				RecentPopulation: topic.Growth.Recent.Population}
			byRubric[rubric] = science
		}
		prior, recent = topic.Growth.Prior.Period, topic.Growth.Recent.Period
		science.Topics++
		science.Prior += topic.Growth.Prior.Count
		science.Recent += topic.Growth.Recent.Count
	}
	out = make([]LiveMoneyScience, 0, len(byRubric))
	for _, science := range byRubric {
		out = append(out, *science)
	}
	slices.SortFunc(out, func(a, b LiveMoneyScience) int { return strings.Compare(a.Rubric, b.Rubric) })
	return out, prior, recent
}
