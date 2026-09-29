package load

import (
	"context"
	"time"

	"github.com/example/llm-service/internal/platform/logger"
	"github.com/example/llm-service/internal/platform/valkey"
)

// Key читает и chunk-splitter, менять только вместе с ним.
const Key = "rag:active_queries"

// Упавший держатель не раздувает счётчик дольше этого срока.
const counterTTL = 30 * time.Second

type Marker struct {
	vk *valkey.Client
}

func New(vk *valkey.Client) *Marker { return &Marker{vk: vk} }

func (m *Marker) QueryStarted(ctx context.Context) {
	if _, err := m.vk.IncrTTL(ctx, Key, counterTTL); err != nil {
		logger.From(ctx).Warn().Err(err).Msg("load marker incr failed")
	}
}

// QueryFinished не чинит отрицательное значение (TTL мог истечь посреди запроса):
// читатели обрезают его по нулю.
func (m *Marker) QueryFinished(ctx context.Context) {
	if _, err := m.vk.Decr(ctx, Key); err != nil {
		logger.From(ctx).Warn().Err(err).Msg("load marker decr failed")
	}
}
