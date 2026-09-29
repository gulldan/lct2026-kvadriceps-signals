package application

import (
	"context"
	"sync"
)

type liveWorkerSlotsKey struct{}
type liveWorkerSlots chan struct{}

type liveWaveWorkers struct {
	calls, ratings liveWorkerSlots
}

// Одновременные стадии всех волн делят бюджет; остальные вызовы сохраняют прежний отдельный предел.
func (s *TrendLiveService) workerSlots(ctx context.Context) liveWorkerSlots {
	if workers, ok := ctx.Value(liveWorkerSlotsKey{}).(*liveWaveWorkers); ok {
		return workers.calls
	}
	return make(liveWorkerSlots, s.llmWorkers(ctx))
}

func (slots liveWorkerSlots) acquire(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case slots <- struct{}{}:
		if ctx.Err() != nil {
			slots.release()
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}

func (slots liveWorkerSlots) release() { <-slots }

// Пакеты готовы в разное время, но не умножают прежний предел восьми оценок зрелости.
func (workers *liveWaveWorkers) acquireRating(ctx context.Context) bool {
	if !workers.ratings.acquire(ctx) {
		return false
	}
	if !workers.calls.acquire(ctx) {
		workers.ratings.release()
		return false
	}
	return true
}

func (workers *liveWaveWorkers) releaseRating() {
	workers.calls.release()
	workers.ratings.release()
}

func (s *TrendLiveService) eachConcurrently(ctx context.Context, n int, work func(i int)) {
	slots := s.workerSlots(ctx)
	var workers sync.WaitGroup
	for i := range n {
		if !slots.acquire(ctx) {
			break
		}
		workers.Go(func() {
			defer slots.release()
			work(i)
		})
	}
	workers.Wait()
}
