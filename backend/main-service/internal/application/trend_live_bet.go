package application

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

// Каждая пересборка ответа сравнивается со снимком момента прогноза; в историю идёт только разница двух ответов.

const (
	liveBetConfirming = "confirming"
	liveBetUnchanged  = "unchanged"
	liveBetWeakening  = "weakening"

	liveBetKindStage     = "stage"
	liveBetKindTrend     = "trend"
	liveBetKindEvent     = "event"
	liveBetKindDownloads = "downloads"

	// По типу источника сверка находит прогнозы, не читая всю фабрику гипотез.
	liveBetSource = "Технологический радар"
	// В колокольчик — только первые изменения: длинную историю человек читает в карточке прогноза.
	liveBetHistoryNews = 5
	liveBetEventsShown = 3
)

// LiveBet хранит в Rest поля паспорта прогноза: сверка переписывает прогноз целиком и не должна их терять.
type LiveBet struct {
	Horizon          string             `json:"horizon"`
	StageCode        int                `json:"stage_code"`
	NextStageTrigger string             `json:"next_stage_trigger"`
	Indicators       []LiveBetIndicator `json:"indicators"`
	// От Baseline — того, что радар знал в момент прогноза, — отсчитываются пороги и «ослабевает».
	Baseline  *LiveBetSnapshot `json:"baseline,omitempty"`
	Snapshot  *LiveBetSnapshot `json:"snapshot,omitempty"`
	Status    string           `json:"status,omitempty"`
	CheckedAt string           `json:"checked_at,omitempty"`
	History   []LiveBetChange  `json:"history,omitempty"`
	Rest      jsontext.Value   `json:",unknown"`
}

type LiveBetIndicator struct {
	Kind      string  `json:"kind"`
	EventType string  `json:"event_type,omitempty"`
	Threshold float64 `json:"threshold"`
	Label     string  `json:"label"`
}

// LiveBetSnapshot — индикаторы в одном ответе радара; Downloads nil — ни один источник их не измерил.
type LiveBetSnapshot struct {
	ObservedAt string         `json:"observed_at"`
	StageCode  int            `json:"stage_code"`
	TrendCode  int            `json:"trend_code"`
	Score      int            `json:"score"`
	Events     map[string]int `json:"events"`
	Downloads  *int           `json:"downloads,omitempty"`
	ModelScore *float64       `json:"model_score,omitempty"`
}

type LiveBetChange struct {
	At    string `json:"at"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Quote string `json:"quote,omitempty"`
	URL   string `json:"url,omitempty"`
}

func liveBetEventTypes() []string {
	return []string{LiveEventDeployment, LiveEventPilot, LiveEventGrantLate, LiveEventProductLaunch,
		LiveEventCodeRelease, LiveEventPrototype, LiveEventGrantEarly, LiveEventFunding, LiveEventHiring, LiveEventStandard,
		LiveEventLabResult}
}

// Код стадии приходит от клиента: вне шкалы он читается как «не установлена», а не роняет сверку.
func liveBetStage(code int) string {
	if code < 0 || code > 4 {
		code = 0
	}
	return liveTZStageName(code)
}

func liveTZTrendName(code int) string {
	switch code {
	case 1:
		return "стабильно"
	case 2:
		return "растёт"
	case 3:
		return "быстро растёт"
	}
	return "не установлен"
}

// Неподтверждённое событие не считается: его цитата не называет технологию.
func liveBetSnapshotOf(candidate LiveTopic, observedAt string) LiveBetSnapshot {
	snapshot := LiveBetSnapshot{ObservedAt: observedAt, Events: map[string]int{}}
	if tz := candidate.TZ; tz != nil {
		snapshot.StageCode, snapshot.TrendCode, snapshot.Score = tz.StageCode, tz.TrendCode, tz.Score
	}
	downloads, measured := 0, false
	for _, event := range candidate.Events {
		if !event.Verified || event.Type == "" {
			continue
		}
		snapshot.Events[event.Type]++
		if event.Count != nil && event.Unit == liveBetKindDownloads {
			downloads += *event.Count
			measured = true
		}
	}
	if measured {
		snapshot.Downloads = &downloads
	}
	if score := candidate.ModelScore; score != nil && score.Label != liveClassifierInsufficient {
		snapshot.ModelScore = new(score.Value)
	}
	return snapshot
}

// Пороги — от текущего: индикатор, выполненный уже в момент прогноза, ничего не сказал бы о будущем.
func liveBetIndicators(snapshot LiveBetSnapshot) []LiveBetIndicator {
	indicators := []LiveBetIndicator{}
	if snapshot.StageCode < 4 {
		next := max(snapshot.StageCode, 1) + 1
		indicators = append(indicators, LiveBetIndicator{Kind: liveBetKindStage, Threshold: float64(next),
			Label: fmt.Sprintf("стадия по шкале ТЗ не ниже %d — %s", next, liveBetStage(next))})
	}
	funding := snapshot.Events[LiveEventFunding]
	indicators = append(indicators, LiveBetIndicator{Kind: liveBetKindEvent, EventType: LiveEventFunding,
		Threshold: float64(funding + 1), Label: fmt.Sprintf("новый раунд финансирования (сейчас раундов: %d)", funding)})
	if snapshot.TrendCode < 3 {
		next := max(snapshot.TrendCode, 1) + 1
		indicators = append(indicators, LiveBetIndicator{Kind: liveBetKindTrend, Threshold: float64(next),
			Label: "тренд упоминаний: " + liveTZTrendName(next)})
	}
	if snapshot.Downloads != nil {
		target := max(int(math.Ceil(float64(*snapshot.Downloads)*1.5)), *snapshot.Downloads+1)
		indicators = append(indicators, LiveBetIndicator{Kind: liveBetKindDownloads, Threshold: float64(target),
			Label: fmt.Sprintf("скачиваний не меньше %d (сейчас %d)", target, *snapshot.Downloads)})
	}
	return indicators
}

func liveBetValue(indicator LiveBetIndicator, snapshot LiveBetSnapshot) float64 {
	switch indicator.Kind {
	case liveBetKindStage:
		return float64(snapshot.StageCode)
	case liveBetKindTrend:
		return float64(snapshot.TrendCode)
	case liveBetKindEvent:
		return float64(snapshot.Events[indicator.EventType])
	case liveBetKindDownloads:
		if snapshot.Downloads != nil {
			return float64(*snapshot.Downloads)
		}
	case "model_score":
		if snapshot.ModelScore != nil {
			return *snapshot.ModelScore
		}
	}
	return 0
}

// Неустановленная стадия — не ослабление: отказ источника — не свидетельство против технологии.
func liveBetStatus(bet LiveBet) string {
	if bet.Snapshot == nil {
		return liveBetUnchanged
	}
	now := *bet.Snapshot
	for _, indicator := range bet.Indicators {
		if liveBetValue(indicator, now) >= indicator.Threshold {
			return liveBetConfirming
		}
	}
	if base := bet.Baseline; base != nil && ((now.StageCode > 0 && now.StageCode < base.StageCode) ||
		(now.TrendCode > 0 && now.TrendCode < base.TrendCode)) {
		return liveBetWeakening
	}
	return liveBetUnchanged
}

// known — ссылки событий, уже известных прогнозу: запись, бывшая при переносе, не выдаётся за новость.
func liveBetObserve(bet *LiveBet, candidate LiveTopic, observedAt string, known map[string]bool,
	now time.Time,
) []LiveBetChange {
	next := liveBetSnapshotOf(candidate, observedAt)
	prev := cmp.Or(bet.Snapshot, bet.Baseline)
	if bet.Baseline == nil {
		bet.Baseline = &next
	}
	// Прогноз по ответу без стадии и тренда получает их первым измерением, и пороги ставятся от него:
	// иначе порог «не ниже второй стадии» от нуля выполнялся бы первой же сверкой.
	if !liveBetMeasured(*bet.Baseline) && liveBetMeasured(next) {
		bet.Baseline, bet.Indicators = &next, liveBetIndicators(next)
	}
	var changes []LiveBetChange
	if prev != nil {
		changes = liveBetDiff(bet.Indicators, *prev, next, candidate, known, now.UTC().Format(time.RFC3339))
	}
	bet.Snapshot = &next
	bet.Status = liveBetStatus(*bet)
	bet.CheckedAt = now.UTC().Format(time.RFC3339)
	bet.History = append(bet.History, changes...)
	return changes
}

func liveBetMeasured(snapshot LiveBetSnapshot) bool {
	return snapshot.StageCode > 0 || snapshot.TrendCode > 0
}

func liveBetDiff(indicators []LiveBetIndicator, prev, next LiveBetSnapshot, candidate LiveTopic,
	known map[string]bool, at string,
) []LiveBetChange {
	changes := liveBetEventChanges(prev, next, candidate, known)
	changes = append(changes, liveBetScaleChanges(prev, next, candidate)...)
	changes = append(changes, liveBetMeasureChanges(indicators, prev, next)...)
	for i := range changes {
		changes[i].At = at
	}
	return changes
}

func liveBetEventChanges(prev, next LiveBetSnapshot, candidate LiveTopic, known map[string]bool) []LiveBetChange {
	changes := []LiveBetChange{}
	for _, eventType := range liveBetEventTypes() {
		grew := next.Events[eventType] - prev.Events[eventType]
		if grew <= 0 {
			continue
		}
		shown := 0
		for _, event := range candidate.Events {
			url := event.Record.URL
			if shown >= min(grew, liveBetEventsShown) || !event.Verified || event.Type != eventType || url == "" ||
				known[url] {
				continue
			}
			known[url] = true
			shown++
			text := "Появилось событие «" + liveEventLabel(eventType) + "»"
			if who := cmp.Or(event.Actor, event.Record.Title); who != "" {
				text += ": " + who
			}
			changes = append(changes, LiveBetChange{Kind: liveBetKindEvent + ":" + eventType, Text: text,
				Quote: event.Quote, URL: url})
		}
		if shown < grew {
			changes = append(changes, LiveBetChange{Kind: liveBetKindEvent + ":" + eventType,
				Text: fmt.Sprintf("Событий «%s»: было %d, стало %d", liveEventLabel(eventType),
					prev.Events[eventType], next.Events[eventType])})
		}
	}
	return changes
}

// Пропавшая оценка не новость: это отказ источника, а не движение технологии.
func liveBetScaleChanges(prev, next LiveBetSnapshot, candidate LiveTopic) []LiveBetChange {
	changes := []LiveBetChange{}
	if next.StageCode > 0 && next.StageCode != prev.StageCode {
		change := LiveBetChange{Kind: liveBetKindStage,
			Text: fmt.Sprintf("Стадия по шкале ТЗ: %d — %s", next.StageCode, liveBetStage(next.StageCode))}
		if prev.StageCode > 0 {
			change.Text = fmt.Sprintf("Стадия по шкале ТЗ сменилась: %s → %s", liveBetStage(prev.StageCode),
				liveBetStage(next.StageCode))
		}
		if tz := candidate.TZ; tz != nil && tz.StageEvent != nil {
			change.Quote, change.URL = tz.StageEvent.Quote, tz.StageEvent.Record.URL
		}
		changes = append(changes, change)
	}
	if next.TrendCode > 0 && next.TrendCode != prev.TrendCode {
		text := "Тренд упоминаний: " + liveTZTrendName(next.TrendCode)
		switch {
		case next.TrendCode == 3 && prev.TrendCode > 0:
			text = "Поток упоминаний перешёл в быстрый рост"
		case prev.TrendCode > 0:
			text = fmt.Sprintf("Тренд упоминаний сменился: %s → %s", liveTZTrendName(prev.TrendCode),
				liveTZTrendName(next.TrendCode))
		}
		if tz := candidate.TZ; tz != nil && tz.TrendBasis != "" {
			text += ". " + tz.TrendBasis
		}
		changes = append(changes, LiveBetChange{Kind: liveBetKindTrend, Text: text})
	}
	return changes
}

func liveBetMeasureChanges(indicators []LiveBetIndicator, prev, next LiveBetSnapshot) []LiveBetChange {
	changes := []LiveBetChange{}
	if before, after := prev.Downloads, next.Downloads; before != nil && after != nil && *before > 0 &&
		math.Abs(float64(*after-*before)) >= float64(*before)/10 {
		changes = append(changes, LiveBetChange{Kind: liveBetKindDownloads, Text: fmt.Sprintf(
			"Скачиваний: было %d, стало %d (%+d\u00a0%%)", *before, *after, (*after-*before)*100 / *before)})
	}
	if before, after := prev.ModelScore, next.ModelScore; before != nil && after != nil &&
		math.Abs(*after-*before) >= 0.1 {
		changes = append(changes, LiveBetChange{Kind: "model_score", Text: fmt.Sprintf(
			"Оценка модели слабого сигнала: было %.0f\u00a0%%, стало %.0f\u00a0%%", *before*100, *after*100)})
	}
	for _, indicator := range indicators {
		before, after := liveBetValue(indicator, prev) >= indicator.Threshold,
			liveBetValue(indicator, next) >= indicator.Threshold
		switch {
		case after && !before:
			changes = append(changes, LiveBetChange{Kind: "indicator", Text: "Сработал индикатор: " + indicator.Label})
		case before && !after:
			changes = append(changes, LiveBetChange{Kind: "indicator",
				Text: "Индикатор больше не выполняется: " + indicator.Label})
		}
	}
	return changes
}

// Topic есть у прогноза из направления карты, Query — у прогноза из запроса.
type liveBetRadar struct {
	Query       string              `json:"query"`
	Topic       string              `json:"topic,omitempty"`
	CandidateID string              `json:"candidate_id"`
	Name        string              `json:"name"`
	Events      []LiveAdoptionEvent `json:"events,omitempty"`
	Bet         *LiveBet            `json:"bet,omitempty"`
}

func liveBetOf(raw string) (liveBetRadar, bool) {
	var detail struct {
		Radar *liveBetRadar `json:"radar"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &detail) != nil || detail.Radar == nil || detail.Radar.Bet == nil {
		return liveBetRadar{}, false
	}
	return *detail.Radar, true
}

// Детали — общий JSON всех контуров фабрики: разбирается и переписывается только путь radar.bet.
func liveBetWrite(raw string, bet LiveBet) (string, error) {
	detail := map[string]jsontext.Value{}
	radar := map[string]jsontext.Value{}
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return "", fmt.Errorf("bet detail: %w", err)
	}
	if err := json.Unmarshal(detail["radar"], &radar); err != nil {
		return "", fmt.Errorf("bet radar: %w", err)
	}
	encoded, err := json.Marshal(bet)
	if err != nil {
		return "", fmt.Errorf("bet encode: %w", err)
	}
	radar["bet"] = encoded
	if detail["radar"], err = json.Marshal(radar); err != nil {
		return "", fmt.Errorf("bet radar encode: %w", err)
	}
	out, err := json.Marshal(detail)
	if err != nil {
		return "", fmt.Errorf("bet detail encode: %w", err)
	}
	return string(out), nil
}

type LiveBetCatalog interface {
	ListHypotheses(ctx context.Context, req *dbv1.ListHypothesesRequest) ([]*commonv1.Hypothesis, error)
	GetHypothesis(ctx context.Context, id string) (*commonv1.Hypothesis, error)
	UpdateHypothesis(ctx context.Context, req *dbv1.UpdateHypothesisRequest) error
}

// versions помнит ответ последней сверки: полуторамегабайтный ответ читается, только когда пересобран.
type liveBetTracker struct {
	catalog  LiveBetCatalog
	mu       sync.Mutex
	versions map[string]string
}

type LiveBetResult struct {
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	Status  string          `json:"status"`
	Changes []LiveBetChange `json:"changes"`
}

type LiveBetReport struct {
	Checked int             `json:"checked"`
	Changed []LiveBetResult `json:"changed"`
}

var ErrLiveBetsOff = errors.New("live bets: tracking disabled")

// SetBets встраивает сверку в цикл подписок: он и так смотрит, какие ответы пересобраны.
func (w *LiveWatches) SetBets(catalog LiveBetCatalog) {
	w.bets = &liveBetTracker{catalog: catalog, versions: map[string]string{}}
}

// CheckBets с пустым owner сверяет прогнозы всех, с пустым id — все прогнозы владельца; force — и непересобранные ответы.
func (w *LiveWatches) CheckBets(ctx context.Context, owner, id string, force bool) (LiveBetReport, error) {
	report := LiveBetReport{Changed: []LiveBetResult{}}
	tracker := w.bets
	if tracker == nil {
		return report, ErrLiveBetsOff
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	// ponytail: каждый проход читает все прогнозы целиком — их десятки; при
	// сотнях хватит сверять только те, чей ответ сменил дату сбора.
	hypotheses, err := tracker.catalog.ListHypotheses(ctx, &dbv1.ListHypothesesRequest{OwnerId: owner,
		SourceType: liveBetSource, Limit: 500})
	if err != nil {
		return report, fmt.Errorf("live bets: list: %w", err)
	}
	versions := w.queryVersions(ctx)
	answers := map[string]*LiveDetail{}
	for _, hypothesis := range hypotheses {
		if ctx.Err() != nil {
			break
		}
		if id != "" && hypothesis.GetId() != id {
			continue
		}
		radar, ok := liveBetOf(hypothesis.GetDetail())
		if !ok {
			continue
		}
		key, version, ready := w.betAnswer(radar, versions)
		if !ready || (!force && tracker.versions[hypothesis.GetId()] == version) {
			continue
		}
		detail := w.betDetail(ctx, key, answers)
		candidate, found := liveBetCandidate(detail, radar)
		if !found {
			tracker.versions[hypothesis.GetId()] = version
			continue
		}
		report.Checked++
		bet := *radar.Bet
		changes := liveBetObserve(&bet, candidate, detail.ObservedAt, liveBetKnown(radar), w.now())
		if err := tracker.save(ctx, hypothesis.GetId(), bet); err != nil {
			slog.Warn("live bets: save", "hypothesis", hypothesis.GetId(), "error", err)
			continue
		}
		tracker.versions[hypothesis.GetId()] = version
		w.noteBet(ctx, hypothesis, bet.Status, changes)
		if len(changes) > 0 {
			report.Changed = append(report.Changed, LiveBetResult{ID: hypothesis.GetId(),
				Title: hypothesis.GetTitle(), Status: bet.Status, Changes: changes})
		}
	}
	return report, nil
}

func (w *LiveWatches) betAnswer(radar liveBetRadar, versions map[string]string) (string, string, bool) {
	if radar.Topic != "" {
		version, ready := w.version(LiveWatchTopic, radar.Topic, nil)
		return LiveWatchTopic + ":" + radar.Topic, version, ready
	}
	query, err := normalizeLiveQuery(radar.Query)
	if err != nil {
		return "", "", false
	}
	version, ready := versions[query]
	return LiveWatchQuery + ":" + query, version, ready
}

func (w *LiveWatches) betDetail(ctx context.Context, key string, answers map[string]*LiveDetail) *LiveDetail {
	if detail, ok := answers[key]; ok {
		return detail
	}
	kind, target, _ := strings.Cut(key, ":")
	var (
		detail LiveDetail
		found  bool
	)
	if kind == LiveWatchTopic {
		detail, found = w.live.Topic(target)
	} else {
		detail, found = w.live.storedSearch(ctx, target, nil)
	}
	answers[key] = nil
	if found {
		answers[key] = &detail
	}
	return answers[key]
}

func liveBetCandidate(detail *LiveDetail, radar liveBetRadar) (LiveTopic, bool) {
	if detail == nil {
		return LiveTopic{}, false
	}
	want := liveWatchTech{ID: radar.CandidateID, Name: radar.Name}
	for _, candidate := range detail.Candidates {
		if want.same(liveWatchTech{ID: candidate.ID, Name: cmp.Or(candidate.Label, candidate.Name)}) {
			return candidate, true
		}
	}
	return LiveTopic{}, false
}

func liveBetKnown(radar liveBetRadar) map[string]bool {
	known := map[string]bool{}
	for _, event := range radar.Events {
		known[event.Record.URL] = true
	}
	for _, change := range radar.Bet.History {
		known[change.URL] = true
	}
	delete(known, "")
	return known
}

// Человек мог поправить гипотезу, пока шла сверка, и его правка важнее; конфликт версии повторит следующая сверка.
func (t *liveBetTracker) save(ctx context.Context, id string, bet LiveBet) error {
	hypothesis, err := t.catalog.GetHypothesis(ctx, id)
	if err != nil {
		return fmt.Errorf("read hypothesis: %w", err)
	}
	detail, err := liveBetWrite(hypothesis.GetDetail(), bet)
	if err != nil {
		return err
	}
	hypothesis.Detail = detail
	if err := t.catalog.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: hypothesis}); err != nil {
		return fmt.Errorf("update hypothesis: %w", err)
	}
	return nil
}

func (w *LiveWatches) noteBet(ctx context.Context, hypothesis *commonv1.Hypothesis, status string,
	changes []LiveBetChange,
) {
	now := w.now()
	err := w.update(ctx, hypothesis.GetOwnerId(), func(records []liveWatchRecord) ([]liveWatchRecord, error) {
		i := liveWatchIndex(records, LiveWatchBet, hypothesis.GetId())
		if i < 0 {
			if len(records) >= liveWatchLimit {
				return records, nil
			}
			records = append(records, liveWatchRecord{Watch: LiveWatch{Kind: LiveWatchBet, Target: hypothesis.GetId(),
				CreatedAt: now}})
			i = len(records) - 1
		}
		record := &records[i]
		record.Watch.Title, record.CheckedAt = hypothesis.GetTitle(), now
		if len(changes) == 0 {
			return records, nil
		}
		digest := cmp.Or(record.Watch.Digest, &LiveWatchDigest{})
		news := cmp.Or(digest.Bet, &LiveWatchBetNews{})
		news.Status = status
		for _, change := range changes {
			news.Changes = append(news.Changes, change.Text)
		}
		news.Changes = news.Changes[max(0, len(news.Changes)-liveBetHistoryNews):]
		digest.Bet = news
		record.Watch.Digest, record.Watch.DigestAt = digest, now
		return records, nil
	})
	if err != nil {
		slog.Warn("live bets: notify", "hypothesis", hypothesis.GetId(), "error", err)
	}
}
