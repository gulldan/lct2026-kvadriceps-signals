package pacing

import (
	"context"
	"time"

	"github.com/example/chunk-splitter/internal/platform/logger"
	"github.com/example/chunk-splitter/internal/platform/valkey"
)

// Ключ обязан совпадать с маркером нагрузки llm-service.
const activeQueriesKey = "rag:active_queries"

const pollEvery = 200 * time.Millisecond

type Pacer struct {
	vk       *valkey.Client
	maxYield time.Duration
}

func New(vk *valkey.Client, maxYield time.Duration) *Pacer {
	if maxYield <= 0 {
		maxYield = 2 * time.Second
	}
	return &Pacer{vk: vk, maxYield: maxYield}
}

func (p *Pacer) Wait(ctx context.Context) {
	deadline := time.Now().Add(p.maxYield)
	for {
		n, err := p.vk.GetInt(ctx, activeQueriesKey)
		if err != nil {
			// При сбое чтения индексация продолжается: сломанный канал сигналов не должен её останавливать.
			logger.From(ctx).Warn().Err(err).Msg("pacer gauge read failed; proceeding")
			return
		}
		if n <= 0 {
			return
		}
		if time.Now().After(deadline) {
			// Документ идёт дальше, даже если запросы ещё есть: непрерывный поток запросов не должен
			// морить индексацию голодом.
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollEvery):
		}
	}
}
