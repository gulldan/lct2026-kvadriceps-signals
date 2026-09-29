package application

import (
	"context"
	"slices"
	"strings"
	"time"
)

const (
	// Поздний старт не должен тратить остаток общего срока только на называние сущности.
	liveCandidateWaveReserve = 2 * time.Minute
	// Используем доступную параллельность до поздних отказов, не создавая лишний барьер между стадиями.
	liveCandidateWaveSize = 48
)

type liveCandidateQueue struct {
	pending   []LiveTopic
	known     []LiveTopic
	seen      map[string]bool
	attempted int
}

func newLiveCandidateQueue(candidates []LiveTopic, rejected []LiveRejection) *liveCandidateQueue {
	queue := &liveCandidateQueue{seen: map[string]bool{}}
	queue.enqueue(candidates)
	queue.rememberRejected(rejected)
	return queue
}

func liveCandidateKey(topic LiveTopic) string {
	return strings.Join(liveWords(strings.TrimPrefix(strings.TrimPrefix(topic.ID, liveExtractPrefix), "ngram:")), " ")
}

func (q *liveCandidateQueue) enqueue(candidates []LiveTopic) {
	for _, candidate := range candidates {
		key := liveCandidateKey(candidate)
		if key == "" {
			key = liveLabelKey(candidate.Name)
		}
		if key == "" || q.seen[key] {
			continue
		}
		q.seen[key] = true
		q.pending = append(q.pending, candidate)
		q.known = append(q.known, candidate)
	}
}

func (q *liveCandidateQueue) remember(candidates []LiveTopic) {
	for _, candidate := range candidates {
		at := slices.IndexFunc(q.known, func(known LiveTopic) bool { return known.ID == candidate.ID })
		if at < 0 {
			q.known = append(q.known, candidate)
		} else {
			q.known[at] = candidate
		}
		q.seen[liveCandidateKey(candidate)] = true
	}
}

func (q *liveCandidateQueue) rememberRejected(rejected []LiveRejection) {
	for _, rejection := range rejected {
		if rejection.topic != nil {
			q.remember([]LiveTopic{*rejection.topic})
		}
	}
}

func (q *liveCandidateQueue) expansionDue() bool {
	return len(q.pending) == 0 || q.attempted > 0 && !liveNamedCandidate(q.pending[0])
}

// Названные технологии проходят отдельно: общий словарь не должен исчерпать бюджет до добора.
func (q *liveCandidateQueue) nextWave(workers int) []LiveTopic {
	count := min(len(q.pending), liveCandidateWaveSize, workers)
	firstFallback := slices.IndexFunc(q.pending[:count], func(candidate LiveTopic) bool {
		return !liveNamedCandidate(candidate)
	})
	if firstFallback > 0 {
		count = firstFallback
	}
	wave := q.pending[:count]
	q.pending = q.pending[count:]
	q.attempted += count
	return wave
}

func liveCandidateTimeLeft(ctx context.Context, minimum time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, bounded := ctx.Deadline()
	return !bounded || time.Until(deadline) >= minimum
}

// До полного допуска кандидат остаётся в очереди, а не в резерве отклонённых карточек.
func (s *TrendLiveService) selectCandidateWaves(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection, []StudyPaper) {
	if liveOffTopicAnswer(*umbrella) {
		for _, candidate := range candidates {
			rejected = append(rejected, liveRejection(candidate, liveRejectOffTopic,
				"собранный корпус не отвечает исходному запросу; добор по нему не выполняется"))
		}
		return nil, rejected, documents
	}
	if metadata == nil {
		metadata = map[string]LivePaperMetadata{}
	}
	queue := newLiveCandidateQueue(candidates, rejected)
	// Волны, карточки и оценки зрелости делят один предел одновременных вызовов. Дополнительное извлечение
	// идёт на своих двух местах (expandExtraction): в очереди за общим местом оно истекало бы.
	ctx = context.WithValue(ctx, liveWorkerSlotsKey{}, &liveWaveWorkers{
		calls: s.workerSlots(ctx), ratings: make(liveWorkerSlots, liveRatingParallel),
	})
	parallel := min(s.llmWorkers(ctx), liveCandidateWaveSize)
	pipeline := &liveCandidatePipeline{modelRequired: s.classifier != nil, parallel: parallel,
		updates: make(chan liveCandidateUpdate, parallel+liveTopCandidates-1)}
	coordinator := liveCandidateCoordinator{query: query, umbrella: umbrella, documents: documents,
		metadata: metadata, periods: periods, rejected: rejected, queue: queue, pipeline: pipeline}
	coordinator.run(ctx, s)
	selected, rejected := pipeline.finish(queue, coordinator.rejected)
	limited := coordinator.budgetLimited || ctx.Err() != nil
	rejected = queue.explainPending(umbrella, len(selected), pipeline.goalCount(selected), limited, rejected)
	rejected = s.scoreTopUpPool(ctx, query, *umbrella, liveTopCandidates-len(selected), rejected,
		coordinator.documents, metadata)
	selected, rejected = liveTopUp(*umbrella, selected, rejected, metadata)
	return selected, rejected, coordinator.documents
}

func (s *TrendLiveService) expandCandidateQueue(ctx context.Context, query string, umbrella *LiveTopic,
	queue *liveCandidateQueue, documents []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, rejected []LiveRejection,
) ([]LiveRejection, bool) {
	if !liveCandidateTimeLeft(ctx, liveExtractExpansionWait+liveCandidateWaveReserve) {
		// На добор времени уже нет, но оставшаяся очередь ещё может пройти обычную волну.
		return rejected, len(queue.pending) == 0
	}
	found := s.expandExtraction(ctx, query, documents, metadata, queue.known)
	if ctx.Err() != nil {
		return rejected, true
	}
	extra, refused := s.extractCandidates(*umbrella, found, documents, metadata, periods)
	extra, refused = s.candidatesOnTopic(ctx, query, umbrella, extra, documents, refused)
	previous := len(queue.pending)
	queue.enqueue(extra)
	queue.pending = slices.Concat(queue.pending[previous:], queue.pending[:previous])
	queue.rememberRejected(refused)
	return append(rejected, refused...), ctx.Err() != nil
}

func (q *liveCandidateQueue) explainPending(umbrella *LiveTopic, selected, completed int, budgetLimited bool,
	rejected []LiveRejection,
) []LiveRejection {
	if len(q.pending) > 0 || budgetLimited {
		reason := "не проверено: исчерпан общий срок или недостаточно времени для следующей полной волны"
		if completed == liveTopCandidates {
			reason = "не проверено: уже отобраны пятнадцать полностью подтверждённых карточек"
		} else {
			umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveRejectBudget, reason,
				map[string]int{"attempted": q.attempted, "pending": len(q.pending), "selected": selected}))
		}
		for _, candidate := range q.pending {
			rejected = append(rejected, liveRejection(candidate, liveRejectBudget, reason))
		}
	}
	return rejected
}

func (s *TrendLiveService) prepareCandidateBatch(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection, []StudyPaper) {
	if ctx.Err() != nil || len(candidates) == 0 {
		return liveUnfinishedCandidates(candidates, rejected, documents)
	}
	candidates, rejected = liveInheritByLabel(*umbrella, candidates, rejected, liveMarketBarOf(*umbrella))
	candidates, rejected = s.candidatesOnTopic(ctx, query, umbrella, candidates, documents, rejected)
	if ctx.Err() != nil || len(candidates) == 0 {
		return liveUnfinishedCandidates(candidates, rejected, documents)
	}
	eventsDue := time.Now().Add(liveEventWait)
	s.askEvents(ctx, candidates, metadata)
	s.marketEvidence(ctx, umbrella, candidates, periods)
	s.takeEvents(ctx, umbrella, candidates, metadata, eventsDue)
	for i := range candidates {
		liveEventWorks(&candidates[i])
	}
	documents = liveWithCandidateRecords(candidates, documents)
	livePrepareAnswerMetadata(candidates, documents, metadata)
	if ctx.Err() != nil {
		return liveUnfinishedCandidates(candidates, rejected, documents)
	}
	return candidates, rejected, documents
}

func (s *TrendLiveService) finishCandidateBatch(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
) ([]LiveTopic, []LiveRejection) {
	classify := s.classifyAsync(ctx, query, umbrella, candidates, documents, metadata)
	s.writeWave(ctx, query, candidates, documents, metadata)
	s.assessEvents(ctx, umbrella, candidates, documents, metadata)
	candidates, rejected := liveAdmitCards(candidates, nil)
	candidates, rejected = classify(candidates, rejected)
	if s.classifier != nil {
		candidates, rejected = liveRequireModelScores(candidates, rejected)
	}
	candidates, rejected = liveDropUmbrella(*umbrella, candidates, rejected)
	s.profileCandidateMechanisms(ctx, umbrella, candidates)
	return candidates, rejected
}

// Сбой одной волны не смешивает измеренный модельный ранг другой волны с оценкой по правилам.
func liveRequireModelScores(candidates []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ModelScore == nil {
			rejected = append(rejected, liveRejection(candidate, liveLimitModelFailed,
				"оценка подключённой модели недоступна; карточка не включена в окончательный рейтинг"))
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, rejected
}

func liveUnfinishedCandidates(candidates []LiveTopic, rejected []LiveRejection, documents []StudyPaper,
) ([]LiveTopic, []LiveRejection, []StudyPaper) {
	for _, candidate := range candidates {
		rejected = append(rejected, liveRejection(candidate, liveRejectBudget,
			"не проверено полностью: общий срок поиска завершился до допуска карточки"))
	}
	return nil, rejected, documents
}
