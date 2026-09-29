package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/valkey"
)

type AnswerCache struct {
	client *valkey.Client
	ttl    time.Duration
}

func NewAnswerCache(client *valkey.Client, ttl time.Duration) *AnswerCache {
	return &AnswerCache{client: client, ttl: ttl}
}

func (c *AnswerCache) Get(ctx context.Context, key string) (domain.Result, bool, error) {
	var res domain.Result
	hit, err := c.client.GetJSON(ctx, key, &res)
	if err != nil {
		return domain.Result{}, false, fmt.Errorf("cache get: %w", err)
	}
	return res, hit, nil
}

func (c *AnswerCache) Set(ctx context.Context, key string, res domain.Result) error {
	if err := c.client.SetJSON(ctx, key, res, c.ttl); err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

// Epoch при сбое Valkey отдаёт 0: теряем инвалидацию, но не запрос.
func (c *AnswerCache) Epoch(ctx context.Context, scope string) int64 {
	n, err := c.client.GetInt(ctx, "rag:corpus_epoch:"+scope)
	if err != nil {
		return 0
	}
	return n
}
