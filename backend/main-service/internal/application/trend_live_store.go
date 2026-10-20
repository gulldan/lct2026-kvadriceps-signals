package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Поиск хранится в базе с собранными записями: перезапуск и вторая реплика не должны заново обходить источники.

type LiveSearchStore interface {
	ReadLiveSearch(context.Context, analysisdoc.LiveSearchRead) (analysisdoc.LiveSearchStored, error)
	SaveLiveSearch(context.Context, analysisdoc.LiveSearchSave) (analysisdoc.LiveSearchStored, error)
	ListLiveSearches(context.Context, analysisdoc.LiveSearchList) (analysisdoc.LiveSearchHistory, error)
}

func (s *TrendLiveService) SetSearchStore(store LiveSearchStore) {
	s.mu.Lock()
	s.searchStore = store
	s.mu.Unlock()
}

func (s *TrendLiveService) searchStoreRef() LiveSearchStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchStore
}

// liveTermsKey сортирует термины: «слабые сигналы в ИИ» и «ИИ и слабые сигналы» дают один сохранённый поиск.
func liveTermsKey(terms []string) string {
	cleaned := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.Join(strings.Fields(term), " "))
		if term != "" && !slices.Contains(cleaned, term) {
			cleaned = append(cleaned, term)
		}
	}
	slices.Sort(cleaned)
	if len(cleaned) == 0 {
		return ""
	}
	return liveSearchContract + "|" + strings.Join(cleaned, "|")
}

// liveSearchContract поднимается выкладкой, меняющей отбор или карточку: иначе из базы вернётся ответ без новых полей.
const liveSearchContract = "search-v32"

func liveStoreKey(query string) string { return liveSearchContract + "|" + query }

// Отказ отдельной волны не мешает кэшу, если последующие волны собрали полный проверенный ответ той же версии.
func (s *TrendLiveService) reusableSearch(detail LiveDetail, query string) bool {
	if !liveValidAnswerScope(detail.Scope, query, detail.Candidates) {
		return false
	}
	return s.reusableMLExtraction(detail.Extraction) && detail.ClassifierVersion == s.classifierVersion &&
		liveUsableStored(detail)
}

// storedSearch отдаёт ответ только на тот же запрос: одни слова бывают у противоположных задач. Свежесть
// не проверяется — устаревший ответ открывается сразу, а обновляет его ночная пересборка.
func (s *TrendLiveService) storedSearch(ctx context.Context, query string, terms []string) (LiveDetail, bool) {
	store := s.searchStoreRef()
	if store == nil {
		return LiveDetail{}, false
	}
	stored, err := store.ReadLiveSearch(ctx, analysisdoc.LiveSearchRead{
		Query: liveStoreKey(query), TermsKey: liveTermsKey(terms), WithVersion: true,
	})
	if err != nil {
		slog.Warn("live search store read", "query", query, "error", err)
		return LiveDetail{}, false
	}
	if !stored.Found || len(stored.Detail) == 0 || stored.Query != liveStoreKey(query) {
		return LiveDetail{}, false
	}
	var detail LiveDetail
	if err := json.Unmarshal(stored.Detail, &detail); err != nil {
		slog.Warn("live search store decode", "query", query, "error", err)
		return LiveDetail{}, false
	}
	liveKeepCardContexts(&detail.Topic)
	for i := range detail.Candidates {
		liveKeepCardContexts(&detail.Candidates[i])
	}
	detail.storeVersion = stored.Version
	detail.storeRunID = detail.RunID
	// Ответ прежней версии модели пригоден: новую оценку он получит при открытии (completeStored), без сбора.
	if !liveValidAnswerScope(detail.Scope, query, detail.Candidates) || !liveUsableStored(detail) {
		return LiveDetail{}, false
	}
	// Счётчик «выше 75 %» пересчитывается: ответ хранится неделями, а правило подсчёта может смениться.
	detail.Counters.ModelAboveThreshold = 0
	for _, candidate := range detail.Candidates {
		if liveModelAbove(candidate) {
			detail.Counters.ModelAboveThreshold++
		}
	}
	return detail, true
}

// Без причины в журнале несохранённый ответ выглядел как пропавший: его искали в базе и запускали сбор заново.
func (s *TrendLiveService) logNotStored(query string, detail LiveDetail) {
	failed := func(limit LiveLimit) bool { return limit.Code == liveLimitModelFailed }
	slog.Warn("live search not stored", "query", query,
		"scope", liveValidAnswerScope(detail.Scope, query, detail.Candidates),
		"ml_extraction", s.reusableMLExtraction(detail.Extraction),
		"classifier", detail.ClassifierVersion == s.classifierVersion,
		"model_failed", slices.ContainsFunc(detail.Limits, failed) || slices.ContainsFunc(detail.Topic.Limits, failed),
		"usable", liveUsableStored(detail), "owner_documents", liveMLOwnerEvidence(detail))
}

func liveUsableStored(detail LiveDetail) bool {
	failed := func(limit LiveLimit) bool { return limit.Code == liveLimitModelFailed }
	return !slices.ContainsFunc(detail.Limits, failed) && !slices.ContainsFunc(detail.Topic.Limits, failed) ||
		liveRecoveredModelAnswer(detail)
}

// История отказов остаётся видимой, но решение о восстановлении опирается на каждую итоговую карточку.
func liveRecoveredModelAnswer(detail LiveDetail) bool {
	if len(detail.Candidates) != liveTopCandidates || detail.ClassifierVersion == "" {
		return false
	}
	for _, candidate := range detail.Candidates {
		score := candidate.ModelScore
		if candidate.Shortfall != nil || !liveVerifiedMechanism(candidate) || !liveCardComplete(candidate) ||
			score == nil || score.Model == "" || score.Version != detail.ClassifierVersion ||
			score.FeatureSchema != LiveClassifierSchema || !liveUnitInterval(score.Value) || !liveUnitInterval(score.Threshold) {
			return false
		}
		if !liveModelDecision(score) {
			return false
		}
	}
	return true
}

// Версия фиксируется до нового сбора, даже если прежний снимок уже просрочен или непригоден для выдачи.
func (s *TrendLiveService) searchVersion(ctx context.Context, query string) (version, runID string, known bool) {
	store := s.searchStoreRef()
	if store == nil {
		return "", "", true
	}
	stored, err := store.ReadLiveSearch(ctx, analysisdoc.LiveSearchRead{Query: liveStoreKey(query), WithVersion: true})
	if err != nil {
		slog.Warn("live search version read", "query", query, "error", err)
		return "", "", false
	}
	var identity struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(stored.Detail, &identity); stored.Found && err == nil {
		runID = identity.RunID
	}
	return stored.Version, runID, !stored.Found || stored.Version != ""
}

// Условная запись защищает и вспомогательные правки, и первый результат долгого сбора другой реплики.
func (s *TrendLiveService) saveSearch(ctx context.Context, query string, detail LiveDetail, expires time.Time,
) (LiveDetail, bool) {
	if !s.reusableSearch(detail, query) || liveMLOwnerEvidence(detail) {
		s.logNotStored(query, detail)
		return detail, true
	}
	store := s.searchStoreRef()
	if store == nil {
		return detail, true
	}
	if detail.storeReadFailed {
		return detail, false
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Сохранённый поиск переиспользует кто угодно, поэтому канал загруженных документов владельца снимается.
	stripped := liveCorpusStripped(detail)
	raw, err := json.Marshal(stripped)
	if err != nil {
		slog.Warn("live search store encode", "query", query, "error", err)
		return detail, false
	}
	summary, err := json.Marshal(liveOverviewTopic(stripped.Topic))
	if err != nil {
		slog.Warn("live search store encode summary", "query", query, "error", err)
		return detail, false
	}
	terms := detail.Topic.Query.Terms
	// Время записи — время сбора: дооценка пересохраняет ответ, и он выглядел бы свежим для пересборки и «Истории».
	observed, err := time.Parse(time.RFC3339, detail.ObservedAt)
	if err != nil {
		observed = s.now()
	}
	stored, err := store.SaveLiveSearch(ctx, analysisdoc.LiveSearchSave{
		IfMatch:       new(detail.storeVersion),
		BaselineRunID: liveSearchBaseline(detail),
		Query:         liveStoreKey(query), TermsKey: liveTermsKey(terms), Terms: terms,
		Papers: detail.Topic.PapersCount, ObservedAt: observed, ExpiresAt: expires,
		Detail: raw, Summary: summary, Records: liveStoredRecords(detail),
	})
	if errors.Is(err, analysisdoc.ErrConflict) || status.Code(err) == codes.Aborted {
		current, found := s.storedSearch(ctx, query, nil)
		if found && current.RunID == detail.RunID {
			current.Topic.Corpus = detail.Topic.Corpus
		}
		return current, found
	}
	if err != nil {
		slog.Warn("live search store save", "query", query, "error", err)
		return detail, false
	}
	detail.storeVersion = stored.Version
	detail.storeRunID = detail.RunID
	s.loadSearchTopics(ctx)
	return detail, true
}

func liveSearchBaseline(detail LiveDetail) string {
	if detail.RunID != detail.storeRunID {
		return detail.storeRunID
	}
	return ""
}

func liveStoredRecords(detail LiveDetail) []analysisdoc.LiveSearchRecord {
	out := make([]analysisdoc.LiveSearchRecord, 0, 64)
	seen := map[string]bool{}
	add := func(channel string, records []LiveSourceRecord) {
		for _, record := range records {
			if record.URL == "" || seen[record.URL] {
				continue
			}
			seen[record.URL] = true
			out = append(out, analysisdoc.LiveSearchRecord{
				URL: record.URL, Channel: channel, Source: record.Source, Title: record.Title,
				PublishedOn: record.Date, Language: record.Language, PublisherType: record.PublisherType,
				Trust: record.Trust, TrustBasis: record.TrustBasis, ContentHash: record.ContentHash,
			})
		}
	}
	if detail.Topic.Patents != nil {
		add("patents", detail.Topic.Patents.Records)
	}
	if detail.Topic.PatentsCN != nil {
		add("patents_cn", detail.Topic.PatentsCN.Records)
	}
	if detail.Topic.Registry != nil {
		add("registry", detail.Topic.Registry.Records)
	}
	if detail.Topic.Research != nil {
		add("research", detail.Topic.Research.Records)
	}
	if detail.Topic.Web != nil {
		add("web", detail.Topic.Web.Records)
	}
	for _, paper := range detail.Papers {
		if paper.URL == "" || seen[paper.URL] {
			continue
		}
		seen[paper.URL] = true
		passport := detail.Passports[paper.ID]
		out = append(out, analysisdoc.LiveSearchRecord{
			URL: paper.URL, Channel: "science", Source: liveSourceOf(paper.ID), Title: paper.Title,
			PublishedOn: paper.Date, Language: passport.Language, PublisherType: passport.PublisherType,
			Trust: passport.Trust, TrustBasis: passport.TrustBasis, ContentHash: paper.ContentHash,
		})
	}
	for _, event := range detail.Topic.External {
		if event.URL == "" || seen[event.URL] {
			continue
		}
		seen[event.URL] = true
		out = append(out, analysisdoc.LiveSearchRecord{
			URL: event.URL, Channel: event.Kind, Source: event.Org, Title: event.Title,
			PublishedOn: event.Date, PublisherType: event.PublisherType,
			Trust: event.Trust, TrustBasis: event.TrustBasis,
		})
	}
	return out
}

func liveSourceOf(id string) string {
	if prefix, _, ok := strings.Cut(id, ":"); ok {
		return prefix
	}
	return ""
}

func (s *TrendLiveService) SearchHistory(ctx context.Context, limit int) (analysisdoc.LiveSearchHistory, error) {
	store := s.searchStoreRef()
	if store == nil {
		return analysisdoc.LiveSearchHistory{Entries: []analysisdoc.LiveSearchEntry{}}, nil
	}
	// Предел применяется после отсева прежних форматов, иначе на странице было бы меньше строк.
	history, err := store.ListLiveSearches(ctx, analysisdoc.LiveSearchList{})
	if err != nil {
		return analysisdoc.LiveSearchHistory{}, err
	}
	// Ответы прошлого формата скрыты: по новому ключу их нет, и они открывались бы новым сбором.
	kept := history.Entries[:0]
	for _, entry := range history.Entries {
		contract, query, ok := strings.Cut(entry.Query, "|")
		if !ok || contract != liveSearchContract {
			continue
		}
		entry.Query = query
		kept = append(kept, entry)
	}
	if limit > 0 && len(kept) > limit {
		kept = kept[:limit]
	}
	history.Entries = kept
	return history, nil
}

type LiveSearchTop struct {
	Query      string           `json:"query"`
	Found      bool             `json:"found"`
	ObservedAt string           `json:"observed_at,omitempty"`
	Candidates []LiveSearchPick `json:"candidates"`
}

// LiveSearchPick называет поля как LiveTopic: интерфейс читает их теми же функциями.
type LiveSearchPick struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Label      string            `json:"label,omitempty"`
	Stage      string            `json:"stage"`
	Confidence *LiveConfidence   `json:"confidence,omitempty"`
	ModelScore *LiveModelScore   `json:"model_score,omitempty"`
	TZ         *LiveTZAssessment `json:"tz,omitempty"`
	Shortfall  *LiveShortfall    `json:"shortfall,omitempty"`
	// Каналов записей в строке нет, а без числа подтверждений вердикт «слабый сигнал» не проверить.
	IndependentSources int `json:"independent_sources"`
}

// SearchTop не запускает сбор и режет ответ до limit технологий: полный ответ весит полтора мегабайта.
func (s *TrendLiveService) SearchTop(ctx context.Context, query string, limit int) (LiveSearchTop, error) {
	query, err := normalizeLiveQuery(query)
	if err != nil {
		return LiveSearchTop{}, err
	}
	top := LiveSearchTop{Query: query, Candidates: []LiveSearchPick{}}
	detail, ok := s.storedSearch(ctx, query, nil)
	if !ok {
		return top, nil
	}
	top.Found, top.ObservedAt = true, detail.ObservedAt
	for _, candidate := range detail.Candidates[:min(max(limit, 1), len(detail.Candidates))] {
		model := candidate.ModelScore
		if model != nil {
			// Вклады признаков нужны отчёту о технологии, а не строке списка.
			model = &LiveModelScore{Label: model.Label, Threshold: model.Threshold, Value: model.Value,
				Model: model.Model, Version: model.Version, DataGap: model.DataGap}
		}
		top.Candidates = append(top.Candidates, LiveSearchPick{
			ID: candidate.ID, Name: candidate.Name, Label: candidate.Label, Stage: candidate.Stage,
			Confidence: candidate.Confidence, ModelScore: model, TZ: candidate.TZ, Shortfall: candidate.Shortfall,
			IndependentSources: liveIndependentSources(candidate),
		})
	}
	return top, nil
}

func (s *TrendLiveService) loadSearchTopics(ctx context.Context) {
	history, err := s.SearchHistory(ctx, 0)
	if err != nil {
		slog.Warn("live search topics", "error", err)
		return
	}
	topics := make([]LiveTopic, 0, len(history.Entries))
	for _, entry := range history.Entries {
		var topic LiveTopic
		// Мусорный запрос мог сохраниться до фильтра на входе.
		if liveJunkQuery(entry.Query) {
			continue
		}
		if len(entry.Summary) == 0 || json.Unmarshal(entry.Summary, &topic) != nil || topic.ID == "" {
			continue
		}
		topics = append(topics, topic)
	}
	s.mu.Lock()
	s.searchTopics = topics
	s.mu.Unlock()
}

// liveRefreshSearches мал: каждый запрос стоит минуты шлюза и сотен обращений к OpenAlex.
const liveRefreshSearches = 10

// liveSearchRefreshAge короче суток, чтобы ответ прошлой ночи считался старым.
const liveSearchRefreshAge = 18 * time.Hour

// liveRefreshNightEnd — час UTC: пересборка начинается в полночь UTC, когда OpenAlex обнуляет суточный бюджет.
const liveRefreshNightEnd = 5

// refreshStoredSearches работает только в API-реплике: нужны и хранилище, и модель для карточек.
func (s *TrendLiveService) refreshStoredSearches(ctx context.Context) {
	store := s.searchStoreRef()
	if store == nil || s.card == nil || s.onDemand {
		return
	}
	now := s.now().UTC()
	day := now.Format(time.DateOnly)
	s.mu.Lock()
	// Один проход за ночь: журнальный бюджет OpenAlex нужен живым поискам.
	if s.refreshingSearches || s.snapshot == nil || s.searchesRefreshedFor == day || now.Hour() >= liveRefreshNightEnd {
		s.mu.Unlock()
		return
	}
	s.refreshingSearches = true
	observedAt := s.snapshot.ObservedAt
	s.searchesRefreshedFor = day
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.refreshingSearches = false
		s.mu.Unlock()
	}()
	snapshotAt, err := time.Parse(time.RFC3339, observedAt)
	if err != nil {
		return
	}
	history, err := store.ListLiveSearches(ctx, analysisdoc.LiveSearchList{})
	if err != nil {
		slog.Warn("live search refresh: history", "error", err)
		return
	}
	for _, query := range liveSearchesToRefresh(history.Entries, snapshotAt, now, s.openedSince(ctx, now.Add(-24*time.Hour))) {
		for ctx.Err() == nil {
			err := s.refreshSearch(ctx, query)
			if !errors.Is(err, ErrLiveSearchBusy) {
				if err != nil {
					slog.Warn("live search refresh", "query", query, "error", err)
				}
				break
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Minute):
			}
		}
	}
}

// livePinnedSearches повторяет QUICK_STARTS из LiveOverview.tsx: их ответ должен открываться из сохранённого.
func livePinnedSearches() []string {
	return []string{
		"слабые сигналы в медицине и диагностике",
		"слабые сигналы в сельском хозяйстве и продовольствии",
		"слабые сигналы в энергетике и хранении энергии",
		"слабые сигналы в климате и экологии",
		"слабые сигналы в новых материалах и химии",
		"слабые сигналы в биотехнологиях и разработке лекарств",
		"слабые сигналы в экономике и социальных науках",
		"слабые сигналы в космосе и астрофизике",
		"слабые сигналы в робототехнике",
		"слабые сигналы в безопасности систем искусственного интеллекта",
	}
}

// liveSearchesToRefresh берёт, кроме закреплённых, только открытые за сутки: бюджет OpenAlex не тратится впустую.
func liveSearchesToRefresh(entries []analysisdoc.LiveSearchEntry, snapshotAt, now time.Time,
	opened map[string]bool,
) []string {
	fresh := map[string]bool{}
	for _, entry := range entries {
		contract, query, _ := strings.Cut(entry.Query, "|")
		recent := !entry.ObservedAt.Before(snapshotAt) || now.Sub(entry.ObservedAt) < liveSearchRefreshAge
		if contract == liveSearchContract && recent {
			fresh[query] = true
		}
	}
	seen := map[string]bool{}
	queries := []string{}
	for _, pinned := range livePinnedSearches() {
		query, err := normalizeLiveQuery(pinned)
		if err != nil {
			continue
		}
		seen[query] = true
		if !fresh[query] {
			queries = append(queries, query)
		}
	}
	stored := 0
	for _, entry := range entries {
		_, query, ok := strings.Cut(entry.Query, "|")
		if !ok || seen[query] {
			continue
		}
		seen[query] = true
		if !fresh[query] && opened[query] && stored < liveRefreshSearches {
			queries = append(queries, query)
			stored++
		}
	}
	return queries
}

// liveOpenedKey держит открытия в хранилище: выкладка не должна их стирать.
const liveOpenedKey = "trends/live/searches-opened-v1.json"

// noteOpened пишет в хранилище не чаще раза в час на запрос: опрос идёт раз в две секунды.
func (s *TrendLiveService) noteOpened(ctx context.Context, query string) {
	s.loadOpened(ctx)
	now := s.now()
	s.mu.Lock()
	if s.opened == nil {
		s.opened = map[string]time.Time{}
	}
	if now.Sub(s.opened[query]) < time.Hour {
		s.mu.Unlock()
		return
	}
	s.opened[query] = now
	maps.DeleteFunc(s.opened, func(_ string, at time.Time) bool { return now.Sub(at) > 7*24*time.Hour })
	raw, err := json.Marshal(s.opened)
	s.mu.Unlock()
	if err != nil || s.store == nil {
		return
	}
	go func() {
		if err := s.store.Put(context.WithoutCancel(ctx), liveOpenedKey, bytes.NewReader(raw), int64(len(raw)),
			"application/json"); err != nil {
			slog.Warn("live search opened not saved", "error", err)
		}
	}()
}

func (s *TrendLiveService) loadOpened(ctx context.Context) {
	s.mu.RLock()
	loaded := s.openedLoaded
	s.mu.RUnlock()
	if loaded || s.store == nil {
		return
	}
	stored := map[string]time.Time{}
	if raw, err := s.store.GetBytes(ctx, liveOpenedKey); err == nil {
		_ = json.Unmarshal(raw, &stored)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opened == nil {
		s.opened = map[string]time.Time{}
	}
	s.openedLoaded = true
	for query, at := range stored {
		if at.After(s.opened[query]) {
			s.opened[query] = at
		}
	}
}

func (s *TrendLiveService) openedSince(ctx context.Context, since time.Time) map[string]bool {
	s.loadOpened(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	for query, at := range s.opened {
		if !at.Before(since) {
			out[query] = true
		}
	}
	return out
}

func (s *TrendLiveService) warmStoredSearches(ctx context.Context) {
	store := s.searchStoreRef()
	s.mu.Lock()
	// Без фона дооценка и пересказы делаются при открытии ответа человеком.
	if s.storedWarmed || store == nil || s.onDemand {
		s.mu.Unlock()
		return
	}
	s.storedWarmed = true
	s.mu.Unlock()
	history, err := store.ListLiveSearches(ctx, analysisdoc.LiveSearchList{})
	if err != nil {
		slog.Warn("live stored answers rescoring: history", "error", err)
		return
	}
	warmed, digested := 0, 0
	for _, query := range liveSearchesToWarm(history.Entries, s.now()) {
		detail, ok := s.storedSearch(ctx, query, nil)
		if !ok || detail.ClassifierVersion == s.classifierVersion && !s.liveIncomplete(detail) {
			continue
		}
		turn, release, ok := s.directionTurn(ctx)
		if !ok {
			return
		}
		rescored, filled, err := s.warmStored(turn, query, detail)
		release()
		if err != nil {
			// Окно сборщика ещё грузится: проход повторится позже и пропустит уже дооценённые ответы.
			slog.Info("live stored answers warm-up waits for the collector window", "error", err)
			s.mu.Lock()
			s.storedWarmed = false
			s.mu.Unlock()
			return
		}
		if rescored {
			warmed++
		}
		if filled {
			digested++
		}
	}
	slog.Info("live stored answers warmed", "rescored", warmed, "digested", digested, "version", s.classifierVersion)
}

// warmStored пересохраняет ответ, только если дооценки не было: rescoreStored сохраняет его сам.
func (s *TrendLiveService) warmStored(ctx context.Context, query string, detail LiveDetail) (rescored, filled bool,
	err error,
) {
	detail, filled, err = s.backfillDigests(ctx, detail)
	if err != nil {
		return false, false, err
	}
	if detail.ClassifierVersion != s.classifierVersion && s.classifier != nil {
		_, rescored = s.rescoreStored(ctx, query, detail)
	}
	if !rescored && filled {
		s.resaveStored(ctx, query, detail)
	}
	return rescored, filled, nil
}

func liveSearchesToWarm(entries []analysisdoc.LiveSearchEntry, now time.Time) []string {
	queries := []string{}
	for _, pinned := range livePinnedSearches() {
		if query, err := normalizeLiveQuery(pinned); err == nil {
			queries = append(queries, query)
		}
	}
	for _, entry := range entries {
		contract, query, ok := strings.Cut(entry.Query, "|")
		if ok && contract == liveSearchContract && now.Sub(entry.ObservedAt) < 7*24*time.Hour &&
			!slices.Contains(queries, query) {
			queries = append(queries, query)
		}
	}
	return queries
}
