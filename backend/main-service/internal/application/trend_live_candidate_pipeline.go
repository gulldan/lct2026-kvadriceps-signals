package application

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
)

type liveCandidateBatchResult struct {
	count      int
	candidates []LiveTopic
	rejected   []LiveRejection
	limits     []LiveLimit
}

type liveCandidateUpdate struct {
	qualification *liveQualificationResult
	batch         liveCandidateBatchResult
}

type liveCandidatePipeline struct {
	modelRequired bool
	updates       chan liveCandidateUpdate
	work          sync.WaitGroup
	parallel      int
	qualifying    int
	inFlight      int
	started       int
	candidates    []LiveTopic
}

// Только координатор меняет общие документы, метаданные и учёт; производители возвращают снимки.
type liveCandidateCoordinator struct {
	query         string
	umbrella      *LiveTopic
	documents     []StudyPaper
	metadata      map[string]LivePaperMetadata
	periods       []LivePeriod
	rejected      []LiveRejection
	queue         *liveCandidateQueue
	pipeline      *liveCandidatePipeline
	expanded      bool
	budgetLimited bool
}

func (c *liveCandidateCoordinator) run(ctx context.Context, service *TrendLiveService) {
	for {
		updates := c.pipeline.receive(false)
		if len(updates) == 0 {
			if c.refill(ctx, service) {
				continue
			}
			if c.pipeline.inFlight == 0 {
				return
			}
			updates = c.pipeline.receive(true)
		}
		c.apply(ctx, service, updates)
	}
}

func (c *liveCandidateCoordinator) refill(ctx context.Context, service *TrendLiveService) bool {
	selected, _ := c.pipeline.selectCandidates(c.queue, nil)
	// Поздний дубль может уменьшить пятнадцать до четырнадцати: это пауза, а не завершение координатора.
	if c.pipeline.goalCount(selected) == liveTopCandidates || c.budgetLimited {
		return false
	}
	if !liveCandidateTimeLeft(ctx, liveCandidateWaveReserve) {
		c.budgetLimited = true
		return false
	}
	room := c.pipeline.capacity()
	if room == 0 {
		return false
	}
	if !c.expanded && service.extract != nil && c.queue.expansionDue() {
		c.expanded = true
		c.rejected, c.budgetLimited = service.expandCandidateQueue(ctx, c.query, c.umbrella, c.queue,
			c.documents, c.metadata, c.periods, c.rejected)
		return true
	}
	if len(c.queue.pending) == 0 {
		return false
	}
	c.pipeline.qualify(ctx, service, c.query, c.queue.nextWave(room), c.documents, c.metadata)
	return true
}

func (c *liveCandidateCoordinator) apply(ctx context.Context, service *TrendLiveService, updates []liveCandidateUpdate) {
	ready := make([]liveQualificationResult, 0, len(updates))
	for _, update := range updates {
		if update.qualification != nil {
			ready = append(ready, *update.qualification)
		} else {
			c.rejected = c.pipeline.accept(update.batch, c.umbrella, c.queue, c.rejected)
		}
	}
	if len(ready) == 0 {
		return
	}
	c.pipeline.qualifying -= len(ready)
	before := len(c.rejected)
	qualified, refused := liveApplyQualifications(c.umbrella, ready, c.periods, c.rejected)
	qualified, c.rejected, c.documents = service.prepareCandidateBatch(ctx, c.query, c.umbrella,
		qualified, c.documents, c.metadata, c.periods, refused)
	c.queue.rememberRejected(c.rejected[before:])
	// Кредит живёт до терминального результата, включая отказ до запуска карточки.
	c.pipeline.inFlight -= len(ready) - len(qualified)
	if len(qualified) > 0 {
		c.pipeline.start(ctx, service, c.query, c.umbrella, qualified, c.documents, c.metadata)
	}
}

// Прежнее перекрытие допускало полную группу поверх четырнадцати ещё не завершённых карточек.
func (p *liveCandidatePipeline) capacity() int {
	return min(p.parallel-p.qualifying, p.parallel+liveTopCandidates-1-p.inFlight)
}

func (p *liveCandidatePipeline) start(ctx context.Context, service *TrendLiveService,
	query string, umbrella *LiveTopic, candidates []LiveTopic, documents []StudyPaper,
	metadata map[string]LivePaperMetadata,
) {
	local := *umbrella
	local.Limits = slices.Clone(umbrella.Limits)
	before := len(local.Limits)
	papers, meta := slices.Clone(documents), maps.Clone(metadata)
	p.work.Go(func() {
		kept, rejected := service.finishCandidateBatch(ctx, query, &local, candidates, papers, meta)
		// Место в общем бюджете уже освобождено; передача результата не удерживает вызов шлюза.
		p.updates <- liveCandidateUpdate{batch: liveCandidateBatchResult{len(candidates), kept, rejected, local.Limits[before:]}}
	})
}

func (p *liveCandidatePipeline) accept(result liveCandidateBatchResult, umbrella *LiveTopic,
	queue *liveCandidateQueue, rejected []LiveRejection,
) []LiveRejection {
	p.inFlight -= result.count
	p.candidates = append(p.candidates, result.candidates...)
	umbrella.Limits = append(umbrella.Limits, result.limits...)
	queue.remember(result.candidates)
	queue.rememberRejected(result.rejected)
	return append(rejected, result.rejected...)
}

func (p *liveCandidatePipeline) goalCount(candidates []LiveTopic) int {
	complete := 0
	for _, candidate := range candidates {
		if liveCardComplete(candidate) && (!p.modelRequired || liveModelDecision(candidate.ModelScore)) {
			complete++
		}
	}
	return complete
}

func (p *liveCandidatePipeline) finish(queue *liveCandidateQueue,
	rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection) {
	p.work.Wait()
	close(p.updates)
	return p.selectCandidates(queue, rejected)
}

func (p *liveCandidatePipeline) selectCandidates(queue *liveCandidateQueue,
	rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection) {
	// Скорость ответа не меняет порядок и не закрепляет промежуточный выбор дубля: рейтинг всегда общий.
	indexes := make(map[string]int, len(queue.known))
	for i, candidate := range queue.known {
		indexes[candidate.ID] = i
	}
	candidates := slices.Clone(p.candidates)
	slices.SortStableFunc(candidates, func(a, b LiveTopic) int { return cmp.Compare(indexes[a.ID], indexes[b.ID]) })
	return liveSelectCandidates(candidates, rejected)
}

// Канал закрывается только после учёта всех результатов: пауза на пятнадцати не завершает производителей.
func (p *liveCandidatePipeline) receive(wait bool) []liveCandidateUpdate {
	updates := []liveCandidateUpdate{}
	if wait {
		updates = append(updates, <-p.updates)
	}
	for {
		select {
		case update := <-p.updates:
			updates = append(updates, update)
		default:
			return updates
		}
	}
}
