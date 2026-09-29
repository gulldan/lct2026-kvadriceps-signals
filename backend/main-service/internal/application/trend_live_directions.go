package application

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"
)

// Отбор по корпусу снимка, а не новым обходом: обход ради сотни направлений съел бы суточный бюджет.

const liveDirectionsPrefix = "trends/live/directions/"

// liveDirectionRules растёт, когда отбор меняется без смены формата выдачи: ответ направления по
// прежним правилам пересчитывается сразу после выкладки, а не на следующем снимке.
const liveDirectionRules = 21

type liveDirectionAnswer struct {
	Scope             *LiveQueryScope              `json:"scope,omitempty"`
	Snapshot          string                       `json:"snapshot"`
	Contract          string                       `json:"contract"`
	Rules             int                          `json:"rules"`
	ClassifierVersion string                       `json:"classifier_version"`
	RunID             string                       `json:"run_id"`
	Candidates        []LiveTopic                  `json:"candidates"`
	Rejected          []LiveRejection              `json:"rejected"`
	Documents         []StudyPaper                 `json:"documents"`
	Metadata          map[string]LivePaperMetadata `json:"metadata"`
	Limits            []LiveLimit                  `json:"limits"`
	Volume            LiveVolume                   `json:"volume"`
}

func liveDirectionDetail(snapshot *LiveSnapshot, topic LiveTopic, answer liveDirectionAnswer) LiveDetail {
	// Копия всех метаданных снимка на каждое открытие стоила бы больше самой карточки.
	metadata := make(map[string]LivePaperMetadata, len(topic.PaperIDs)+len(answer.Metadata))
	for _, id := range topic.PaperIDs {
		if meta, ok := snapshot.PaperMetadata[id]; ok {
			metadata[id] = meta
		}
	}
	maps.Copy(metadata, answer.Metadata)
	topic.Limits = slices.Concat(topic.Limits, answer.Limits)
	detail := liveDetail(snapshot, topic, snapshot.Papers, answer.Documents, answer.Candidates, answer.Rejected, metadata)
	detail.Scope = liveCloneQueryScope(answer.Scope)
	detail.Volume = answer.Volume
	detail.SearchContract = answer.Contract
	detail.ClassifierVersion = answer.ClassifierVersion
	detail.RunID = answer.RunID
	return detail
}

func (s *TrendLiveService) answerDirection(ctx context.Context, snapshot *LiveSnapshot, source LiveTopic,
) (liveDirectionAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, liveSearchBudget)
	defer cancel()
	runID := rand.Text()
	ctx = context.WithValue(ctx, liveLLMRunKey{}, runID)
	ctx, tally := liveWithTally(ctx)
	// Снимок читают все запросы карточек, а отбор дописывает в тему и метаданные: работает с копиями.
	raw, err := json.Marshal(source)
	if err != nil {
		return liveDirectionAnswer{}, fmt.Errorf("live direction: copy topic: %w", err)
	}
	var topic LiveTopic
	if err = json.Unmarshal(raw, &topic); err != nil {
		return liveDirectionAnswer{}, fmt.Errorf("live direction: copy topic: %w", err)
	}
	inherited := len(topic.Limits)
	metadata := maps.Clone(snapshot.PaperMetadata)
	own := make(map[string]bool, len(topic.PaperIDs))
	for _, id := range topic.PaperIDs {
		own[id] = true
	}
	papers := make([]StudyPaper, 0, len(topic.PaperIDs))
	for _, paper := range snapshot.Papers {
		if own[paper.ID] {
			papers = append(papers, paper)
		}
	}
	query := cmp.Or(topic.Label, topic.Name)
	plan, err := s.planQuery(ctx, query)
	if err != nil {
		return liveDirectionAnswer{}, err
	}
	ctx = liveWithQueryScope(ctx, plan.Scope)
	candidates, rejected, documents := s.shortlist(ctx, query, &topic, papers, metadata, snapshot.Periods, nil, nil)
	candidates, rejected, documents = s.finishCandidates(ctx, query, &topic, candidates, documents, metadata,
		snapshot.Periods, rejected)
	// Страница показывает все работы технологии, а волна карточек пересказывает добранным только источники карточки.
	digests, applyDigests := s.digestJobs(ctx, candidates, documents, metadata)
	s.eachConcurrently(ctx, len(digests), func(i int) { digests[i]() })
	applyDigests()
	// Истёкший срок — итог, как у ответа на запрос: отобранное сохраняется, а недостающие пересказы допишет
	// открытие направления. Иначе направление, не набравшее пятнадцати карточек за срок, не получало ответа.
	if err := ctx.Err(); errors.Is(err, context.Canceled) {
		return liveDirectionAnswer{}, fmt.Errorf("live direction: selection canceled: %w", err)
	}
	detail := liveDetail(snapshot, topic, papers, documents, candidates, rejected, metadata)
	// Просмотрено — всё окно arXiv, как «просмотрено в окне» у ответа на запрос.
	scanned := 0
	for _, period := range snapshot.Periods {
		scanned += period.Population
	}
	answer := liveDirectionAnswer{Scope: liveCloneQueryScope(plan.Scope), Snapshot: snapshot.ObservedAt,
		Contract: liveSearchContract, Rules: liveDirectionRules,
		ClassifierVersion: s.classifierVersion, RunID: runID, Candidates: detail.Candidates,
		Rejected: rejected, Documents: liveEvidenceDocuments(candidates, documents),
		Metadata: map[string]LivePaperMetadata{}, Limits: topic.Limits[inherited:],
		Volume: liveVolume(detail, metadata, scanned, tally)}
	for _, candidate := range candidates {
		for _, id := range slices.Concat(candidate.PaperIDs, candidate.EvidenceIDs) {
			if meta, ok := metadata[id]; ok {
				answer.Metadata[id] = meta
			}
		}
	}
	return answer, nil
}

// Ответ прежнего контракта, записанный ещё без области запроса, остаётся на экране до пересчёта: сотня
// направлений пересчитывается часами и уступает каждому поиску, а пустая вкладка на это время хуже
// прежнего отбора. Текущий контракт без области или с областью другого названия не показываем.
func liveDirectionShown(answer liveDirectionAnswer, topic LiveTopic) bool {
	return answer.Scope == nil && answer.Contract != liveSearchContract ||
		liveValidAnswerScope(answer.Scope, cmp.Or(topic.Label, topic.Name), answer.Candidates)
}

func (s *TrendLiveService) freshDirection(answer liveDirectionAnswer, snapshot *LiveSnapshot) bool {
	failed := func(limit LiveLimit) bool { return limit.Code == liveLimitModelFailed }
	return answer.Scope != nil && liveValidAnswerScope(answer.Scope, answer.Scope.Query, answer.Candidates) &&
		answer.Snapshot == snapshot.ObservedAt && answer.Contract == liveSearchContract &&
		answer.Rules == liveDirectionRules &&
		answer.ClassifierVersion == s.classifierVersion && !slices.ContainsFunc(answer.Limits, failed)
}

// Только в API-реплике: у сборщика нет модели для карточек.
func (s *TrendLiveService) refreshDirections(ctx context.Context) {
	if s.card == nil {
		return
	}
	s.mu.Lock()
	if s.refreshingDirections || s.snapshot == nil {
		s.mu.Unlock()
		return
	}
	s.refreshingDirections = true
	snapshot := s.snapshot
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.refreshingDirections = false
		s.mu.Unlock()
	}()
	lanes := make(chan struct{}, liveDirectionParallel)
	var running sync.WaitGroup
	defer running.Wait()
	// Готовые ответы направлений поднимаются до прогрева: это чтение из хранилища, а прогрев ждёт паузы
	// между поисками и мог бы задержать их на часы.
	topics := slices.Concat(snapshot.Topics, snapshot.Discovered)
	s.loadDirections(ctx, topics)
	// Карта по умолчанию показывает только слабые сигналы: их технологии открывают первыми.
	slices.SortStableFunc(topics, func(a, b LiveTopic) int {
		return cmp.Compare(liveDirectionOrder(a), liveDirectionOrder(b))
	})
	for _, topic := range topics {
		select {
		case lanes <- struct{}{}:
		case <-ctx.Done():
			return
		}
		s.mu.RLock()
		current := s.snapshot
		previous, known := s.directions[topic.ID]
		tried := s.directionTried[topic.ID] == snapshot.ObservedAt
		s.mu.RUnlock()
		// Сменившийся снимок начнёт свой проход со следующей проверки.
		if current != snapshot || ctx.Err() != nil {
			return
		}
		valid := known && liveValidAnswerScope(previous.Scope, cmp.Or(topic.Label, topic.Name), previous.Candidates)
		if tried || valid && s.freshDirection(previous, snapshot) {
			<-lanes
			continue
		}
		turn, release, ok := s.directionTurn(ctx)
		if !ok {
			return
		}
		running.Go(func() {
			defer func() { <-lanes }()
			defer release()
			s.recountDirection(turn, snapshot, topic, previous, known)
		})
	}
}

// Один пересчёт держит в среднем 23 одновременных вызова шлюза (пик 35), а шлюз без очереди тянет около
// 64 на все сервисы стенда: два направления разом вдвое быстрее, третье упёрлось бы в очередь и сроки.
const liveDirectionParallel = 2

func liveDirectionOrder(topic LiveTopic) int {
	if topic.Stage == liveStageEmerging || topic.Stage == liveStageEarly {
		return 0
	}
	return 1
}

// Все сразу: иначе направление в конце списка ждало бы часы, хотя его ответ уже в хранилище.
func (s *TrendLiveService) loadDirections(ctx context.Context, topics []LiveTopic) {
	for _, topic := range topics {
		s.mu.RLock()
		_, known := s.directions[topic.ID]
		s.mu.RUnlock()
		if known {
			continue
		}
		raw, err := s.store.GetBytes(ctx, liveDirectionsPrefix+topic.ID+".json")
		var answer liveDirectionAnswer
		if err != nil || json.Unmarshal(raw, &answer) != nil || !liveDirectionShown(answer, topic) {
			continue
		}
		s.mu.Lock()
		s.directions[topic.ID] = answer
		s.mu.Unlock()
	}
}

// Проверка и взвод отмены — под одной блокировкой, иначе начавшийся между ними поиск шёл бы вместе с фоном.
func (s *TrendLiveService) directionTurn(ctx context.Context) (context.Context, func(), bool) {
	for {
		s.mu.Lock()
		if !s.personBusy() {
			turn, cancel := context.WithCancel(liveInBackground(ctx))
			s.backgroundSeq++
			id := s.backgroundSeq
			s.background[id] = cancel
			s.mu.Unlock()
			return turn, func() {
				s.mu.Lock()
				delete(s.background, id)
				s.mu.Unlock()
				cancel()
			}, true
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, nil, false
		case <-time.After(time.Minute):
		}
	}
}

// Вызывается под блокировкой.
func (s *TrendLiveService) personBusy() bool {
	return s.people > 0 || slices.ContainsFunc(slices.Collect(maps.Values(s.searchJobs)),
		func(job *liveSearchJob) bool {
			return job.state.Status == LiveSearchRunning && job.stored == nil && !job.background
		})
}

// Прерванная работа вернётся в следующую паузу. Вызывается под блокировкой.
func (s *TrendLiveService) yieldBackground() {
	for _, cancel := range s.background {
		cancel()
	}
}

// Interactive прерывает фон: справка и черновик прогноза не должны стоять за проверками пересчёта.
func (s *TrendLiveService) Interactive() func() {
	s.mu.Lock()
	s.people++
	s.yieldBackground()
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.people--
		s.mu.Unlock()
	}
}

func (s *TrendLiveService) recountDirection(ctx context.Context, snapshot *LiveSnapshot, topic LiveTopic,
	previous liveDirectionAnswer, known bool,
) {
	answer, err := s.answerDirection(ctx, snapshot, topic)
	// Прерванный поиском пересчёт не считается попыткой.
	if errors.Is(err, context.Canceled) {
		slog.Info("live direction technologies yielded to a search", "topic", topic.ID)
		return
	}
	s.mu.Lock()
	s.directionTried[topic.ID] = snapshot.ObservedAt
	s.mu.Unlock()
	if err != nil {
		slog.Warn("live direction technologies", "topic", topic.ID, "error", err)
		return
	}
	// Пустая выдача поверх прежней с технологиями — отказ прохода, а не новое знание о направлении. Целиком
	// добранная — тоже: ни одна технология не прошла проверки, а прежние прошли.
	passed := func(candidates []LiveTopic) bool {
		return slices.ContainsFunc(candidates, func(topic LiveTopic) bool { return topic.Shortfall == nil })
	}
	if known && len(previous.Candidates) > 0 && !passed(answer.Candidates) &&
		(len(answer.Candidates) == 0 || passed(previous.Candidates)) {
		slog.Warn("live direction technologies kept the previous answer", "topic", topic.ID,
			"previous_candidates", len(previous.Candidates))
		return
	}
	// Сначала кодирование, потом память: ответ, который не кодируется (NaN в числе), ломал бы карточку
	// направления до перезапуска, хотя в хранилище лежит годный прежний.
	raw, err := json.Marshal(answer)
	if err != nil {
		slog.Warn("live direction technologies encode", "topic", topic.ID, "error", err)
		return
	}
	s.mu.Lock()
	s.directions[topic.ID] = answer
	s.mu.Unlock()
	if err := s.store.Put(ctx, liveDirectionsPrefix+topic.ID+".json", bytes.NewReader(raw), int64(len(raw)),
		"application/json"); err != nil {
		slog.Warn("live direction technologies store", "topic", topic.ID, "error", err)
	}
	slog.Info("live direction technologies", "topic", topic.ID, "candidates", len(answer.Candidates),
		"rejected", len(answer.Rejected))
}
