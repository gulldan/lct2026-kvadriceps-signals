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
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Карта мира: наука по странам аффилиаций OpenAlex для верха мирового топа радара за два полугодия.
// В снимке радара страны нет — arXiv их не отдаёт, поэтому страны докупаются у OpenAlex и хранятся в S3:
// полугодие не меняется, и после выкладки его незачем спрашивать заново.

const (
	liveMapKey      = "trends/live/map/science-countries-v1.json.gz"
	liveMapTopics   = 60
	liveMapSignals  = 15
	liveMapMonths   = 6
	liveMapRefresh  = 24 * time.Hour
	liveMapRetry    = time.Hour
	liveMapBudget   = 10 * time.Minute
	liveMapParallel = 2
	liveMapLead     = 5
	// Запрос оператора на карте открывается своим ответом, карточки направления у него нет.
	liveMapSourceSearch = "search"
)

type LiveMapWindow struct {
	Total     int            `json:"total"`
	Countries map[string]int `json:"countries"`
}

type LiveMapScience struct {
	Term   string        `json:"term"`
	Prior  LiveMapWindow `json:"prior"`
	Recent LiveMapWindow `json:"recent"`
}

// LiveMapBoard — прошлое полугодие [From, Mid), текущее [Mid, To); даты в формате OpenAlex, To не включается.
type LiveMapBoard struct {
	BuiltAt time.Time                 `json:"built_at"`
	From    string                    `json:"from"`
	Mid     string                    `json:"mid"`
	To      string                    `json:"to"`
	Topics  map[string]LiveMapScience `json:"topics"`
}

type LiveMapSignal struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	// Source «search» открывается своим сохранённым ответом, а не карточкой снимка.
	Source string `json:"source"`
	Stage  string `json:"stage"`
	// Technologies — сколько технологий в посчитанной карточке направления; nil — карточка ещё не считалась.
	Technologies  *int     `json:"technologies"`
	Branch        string   `json:"branch,omitempty"`
	Industry      string   `json:"industry,omitempty"`
	FirstYear     string   `json:"first_year,omitempty"`
	Terms         []string `json:"terms"`
	RadarPrior    int      `json:"radar_prior"`
	RadarRecent   int      `json:"radar_recent"`
	Groups        int      `json:"groups"`
	NewGroups     int      `json:"new_groups"`
	ShareRatio    *float64 `json:"share_ratio"`
	SciencePrior  int      `json:"science_prior"`
	ScienceRecent int      `json:"science_recent"`
	// Lead — страны-лидеры науки по убыванию работ за текущее полугодие, код ISO 3166-1 alpha-2.
	Lead  []LiveMapCountryCount `json:"lead"`
	Money LiveMapMoney          `json:"money"`
}

type LiveMapCountryCount struct {
	Country string `json:"country"`
	Count   int    `json:"count"`
}

type LiveMapMoney struct {
	Total int            `json:"total"`
	Kinds map[string]int `json:"kinds"`
	// Stages и Investors — только у денег направления в обзоре: стадии раундов и самые частые фонды.
	Stages    map[string]int `json:"stages,omitempty"`
	Investors []string       `json:"investors,omitempty"`
}

type LiveMapCountry struct {
	Prior  int `json:"prior"`
	Recent int `json:"recent"`
}

type LiveMapSummary struct {
	Status  string `json:"status"`
	BuiltAt string `json:"built_at,omitempty"`
	From    string `json:"from,omitempty"`
	Mid     string `json:"mid,omitempty"`
	To      string `json:"to,omitempty"`
	// Topics — сколько направлений опрошено: страны на карте — сумма только по ним.
	Topics    int                       `json:"topics"`
	Signals   []LiveMapSignal           `json:"signals"`
	Countries map[string]LiveMapCountry `json:"countries"`
}

type liveMapIndex struct {
	mu         sync.Mutex
	board      *LiveMapBoard
	read       bool
	refreshing bool
	warming    bool
	failedAt   time.Time
}

func (s *TrendLiveService) keepMap(ctx context.Context) {
	topics := s.mapTopics()
	index := &s.eventData.worldMap
	index.mu.Lock()
	defer index.mu.Unlock()
	if !index.read {
		index.read = true
		index.board = s.readMap(ctx)
	}
	now := s.now()
	from, mid, to := liveMapWindows(now)
	// Новое направление в верхе топа без стран дождалось бы суточной пересборки и не попало бы на карту.
	missing := index.board != nil && slices.ContainsFunc(topics, func(topic LiveTopic) bool {
		_, ok := index.board.Topics[topic.ID]
		return !ok && topic.Name != ""
	})
	stale := index.board == nil || now.Sub(index.board.BuiltAt) > liveMapRefresh ||
		missing && now.Sub(index.board.BuiltAt) > liveMapRetry ||
		index.board.From != from || index.board.Mid != mid || index.board.To != to
	if stale && !index.refreshing && now.Sub(index.failedAt) > liveMapRetry {
		index.refreshing = true
		go s.refreshMap(ctx)
		return
	}
	go s.warmMapDirections(ctx)
}

// warmMapDirections считает карточки только верха карты мира: фон для всех направлений выключен с 12.10,
// а точка на карте без технологий выглядела пустой, пока карточку не открыли и не подождали. Считается
// тем же путём, что и по открытию — один раз на снимок, в паузах между поисками — и не больше двух
// карточек разом, как у фонового прохода.
func (s *TrendLiveService) warmMapDirections(ctx context.Context) {
	if !s.onDemand {
		return
	}
	index := &s.eventData.worldMap
	index.mu.Lock()
	if index.warming {
		index.mu.Unlock()
		return
	}
	index.warming = true
	index.mu.Unlock()
	defer func() {
		index.mu.Lock()
		index.warming = false
		index.mu.Unlock()
	}()
	for _, signal := range s.MapSummary().Signals {
		if signal.Source == liveMapSourceSearch {
			continue
		}
		for {
			s.mu.RLock()
			busy := len(s.directionCounting)
			s.mu.RUnlock()
			if busy < liveDirectionParallel {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
		s.countOnOpen(ctx, signal.ID)
	}
}

// liveMapWindows — два последних полных полугодия: текущий неполный месяц в сравнение не входит.
func liveMapWindows(now time.Time) (string, string, string) {
	to := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	mid := to.AddDate(0, -liveMapMonths, 0)
	return mid.AddDate(0, -liveMapMonths, 0).Format(time.DateOnly), mid.Format(time.DateOnly), to.Format(time.DateOnly)
}

func (s *TrendLiveService) readMap(ctx context.Context) *LiveMapBoard {
	if s.store == nil {
		return nil
	}
	raw, err := s.store.GetBytes(ctx, liveMapKey)
	if err != nil {
		return nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	defer func() { _ = reader.Close() }()
	var board LiveMapBoard
	if json.UnmarshalRead(reader, &board) != nil || board.Topics == nil {
		return nil
	}
	return &board
}

// refreshMap докупает страны только новым направлениям окна: сохранённые полугодия не меняются.
func (s *TrendLiveService) refreshMap(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, liveMapBudget)
	defer cancel()
	index := &s.eventData.worldMap
	now := s.now()
	from, mid, to := liveMapWindows(now)
	index.mu.Lock()
	previous := index.board
	index.mu.Unlock()
	board := &LiveMapBoard{BuiltAt: now, From: from, Mid: mid, To: to, Topics: map[string]LiveMapScience{}}
	if previous != nil && previous.From == from && previous.Mid == mid && previous.To == to {
		for id, science := range previous.Topics {
			board.Topics[id] = science
		}
	}
	err := s.collectMapScience(ctx, board)
	if err == nil && len(board.Topics) > 0 {
		err = s.saveMap(ctx, board)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	index.refreshing = false
	if len(board.Topics) > 0 {
		index.board = board
	}
	if err != nil || len(board.Topics) == 0 {
		index.failedAt = s.now()
		slog.Warn("world map science refresh failed", "topics", len(board.Topics), "error", err)
		return
	}
	slog.Info("world map science refreshed", "topics", len(board.Topics))
	s.warmMapDirections(ctx)
}

// mapTopics — верх мирового топа: слабые сигналы первыми, внутри — по росту доли.
func (s *TrendLiveService) mapTopics() []LiveTopic {
	s.mu.RLock()
	topics := []LiveTopic{}
	if s.snapshot != nil {
		topics = slices.Concat(s.snapshot.Topics, s.snapshot.Discovered)
	}
	s.mu.RUnlock()
	ratio := func(topic *LiveTopic) float64 {
		if topic.Growth.ShareRatio == nil {
			return 0
		}
		return *topic.Growth.ShareRatio
	}
	slices.SortStableFunc(topics, func(a, b LiveTopic) int {
		return cmp.Or(cmp.Compare(liveMapWeakRank(a.Stage), liveMapWeakRank(b.Stage)), cmp.Compare(ratio(&b), ratio(&a)))
	})
	return topics[:min(len(topics), liveMapTopics)]
}

func liveMapWeakRank(stage string) int {
	if stage == liveStageEarly || stage == liveStageEmerging {
		return 0
	}
	return 1
}

func (s *TrendLiveService) collectMapScience(ctx context.Context, board *LiveMapBoard) error {
	if s.oa == nil {
		return errors.New("world map: OpenAlex не настроен")
	}
	var errs []error
	var mu sync.Mutex
	var wg sync.WaitGroup
	slot := make(chan struct{}, liveMapParallel)
	for _, topic := range s.mapTopics() {
		if _, ok := board.Topics[topic.ID]; ok || topic.Name == "" {
			continue
		}
		if err := liveScholarReserved(s.oa); err != nil {
			errs = append(errs, err)
			break
		}
		slot <- struct{}{}
		wg.Go(func() {
			defer func() { <-slot }()
			science := LiveMapScience{Term: topic.Name}
			prior, err := liveMapCountries(ctx, s.oa, topic.Name, board.From, board.Mid)
			if err == nil {
				science.Prior = prior
				science.Recent, err = liveMapCountries(ctx, s.oa, topic.Name, board.Mid, board.To)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%q: %w", topic.Name, err))
				return
			}
			board.Topics[topic.ID] = science
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// liveMapCountries — страны аффилиаций по фразе за [from, to): группировка стоит десять кредитов за запрос.
func liveMapCountries(ctx context.Context, client interface {
	Get(context.Context, string, url.Values) ([]byte, error)
}, phrase, from, to string) (LiveMapWindow, error) {
	clean := strings.Join(strings.FieldsFunc(phrase, func(r rune) bool {
		return r == ',' || r == '"' || r == '|' || unicode.IsSpace(r)
	}), " ")
	last, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return LiveMapWindow{}, fmt.Errorf("страны аффилиаций: окно: %w", err)
	}
	filter := fmt.Sprintf(`title_and_abstract.search:"%s",from_publication_date:%s,to_publication_date:%s`,
		clean, from, last.AddDate(0, 0, -1).Format(time.DateOnly))
	raw, err := client.Get(ctx, "/works",
		url.Values{oaFilter: {filter}, "group_by": {"authorships.countries"}, livePriorPage: {"200"}})
	if err != nil {
		return LiveMapWindow{}, fmt.Errorf("страны аффилиаций: %w", err)
	}
	var answer struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		GroupBy []struct {
			Key   string `json:"key"`
			Count int    `json:"count"`
		} `json:"group_by"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return LiveMapWindow{}, fmt.Errorf("страны аффилиаций: %w", err)
	}
	window := LiveMapWindow{Total: answer.Meta.Count, Countries: map[string]int{}}
	for _, group := range answer.GroupBy {
		// Ключ группы — адрес «https://openalex.org/countries/US»; без страны группа подписана «unknown».
		code := strings.ToUpper(group.Key[strings.LastIndex(group.Key, "/")+1:])
		if len(code) == 2 && group.Count > 0 {
			window.Countries[code] = group.Count
		}
	}
	return window, nil
}

func (s *TrendLiveService) saveMap(ctx context.Context, board *LiveMapBoard) error {
	if s.store == nil {
		return nil
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if err := json.MarshalWrite(writer, board); err != nil {
		return fmt.Errorf("world map: encode: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("world map: compress: %w", err)
	}
	if err := s.store.Put(ctx, liveMapKey, bytes.NewReader(buf.Bytes()), int64(buf.Len()), "application/gzip"); err != nil {
		return fmt.Errorf("world map: persist: %w", err)
	}
	return nil
}

func (s *TrendLiveService) mapBoard() (*LiveMapBoard, string) {
	index := &s.eventData.worldMap
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

// MapSummary собирает сигналы карты из снимка радара, купленных стран и доски ранних денег.
func (s *TrendLiveService) MapSummary() LiveMapSummary {
	board, status := s.mapBoard()
	summary := LiveMapSummary{Status: status, Signals: []LiveMapSignal{}, Countries: map[string]LiveMapCountry{}}
	if board == nil {
		return summary
	}
	summary.BuiltAt, summary.From, summary.Mid, summary.To = board.BuiltAt.Format(time.RFC3339), board.From, board.Mid, board.To
	topics := s.mapTopics()
	technologies := map[string]int{}
	s.mu.RLock()
	for _, topic := range topics {
		if answer, ok := s.directions[topic.ID]; ok {
			technologies[topic.ID] = len(answer.Candidates)
		}
	}
	s.mu.RUnlock()
	for _, topic := range topics {
		science, ok := board.Topics[topic.ID]
		if !ok {
			continue
		}
		summary.Topics++
		for code, n := range science.Recent.Countries {
			country := summary.Countries[code]
			country.Recent += n
			summary.Countries[code] = country
		}
		for code, n := range science.Prior.Countries {
			country := summary.Countries[code]
			country.Prior += n
			summary.Countries[code] = country
		}
	}
	industries, rubrics := liveFamilyIndustries(), liveRubrics()
	industry := func(topic LiveTopic) string { return liveTopicIndustry(topic, industries, rubrics) }
	// Сигнал без стран тоже в списке: страны докупаются у OpenAlex по бюджету, а числа радара в списке свои.
	for _, topic := range liveMapDiverse(topics, industry)[:min(len(topics), liveMapSignals)] {
		science := board.Topics[topic.ID]
		lead := make([]LiveMapCountryCount, 0, len(science.Recent.Countries))
		for code, n := range science.Recent.Countries {
			lead = append(lead, LiveMapCountryCount{Country: code, Count: n})
		}
		slices.SortFunc(lead, func(a, b LiveMapCountryCount) int {
			return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Country, b.Country))
		})
		terms := slices.Concat([]string{topic.Name}, topic.Query.Terms)
		match := s.MoneyMatch(terms)
		var computed *int
		if n, ok := technologies[topic.ID]; ok {
			computed = &n
		}
		summary.Signals = append(summary.Signals, LiveMapSignal{
			ID: topic.ID, Name: topic.Name, Label: topic.Label, Source: topic.Source, Stage: topic.Stage, Branch: topic.Branch,
			Industry:     industry(topic),
			Technologies: computed,
			FirstYear:    topic.FirstYear, Terms: terms,
			RadarPrior: topic.Growth.Prior.Count, RadarRecent: topic.Growth.Recent.Count,
			Groups: topic.Groups, NewGroups: topic.Growth.Recent.NewGroups, ShareRatio: topic.Growth.ShareRatio,
			SciencePrior: science.Prior.Total, ScienceRecent: science.Recent.Total,
			Lead:  lead[:min(len(lead), liveMapLead)],
			Money: LiveMapMoney{Total: match.Total, Kinds: match.Kinds},
		})
	}
	return summary
}

// liveMapDiverse ставит вперёд первое направление каждой отрасли в порядке топа, остальные — следом: иначе
// пятнадцать мест списка забирают вычисления, у которых больше всего статей.
func liveMapDiverse(topics []LiveTopic, industry func(LiveTopic) string) []LiveTopic {
	seen := map[string]bool{}
	first, rest := []LiveTopic{}, []LiveTopic{}
	for _, topic := range topics {
		if key := industry(topic); !seen[key] {
			seen[key] = true
			first = append(first, topic)
		} else {
			rest = append(rest, topic)
		}
	}
	return slices.Concat(first, rest)
}
