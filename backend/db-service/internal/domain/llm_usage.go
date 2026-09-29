package domain

import (
	"context"
	"time"
)

type LLMUsageDaily struct {
	Day              time.Time
	Model            string
	Operation        string
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
	CostNanoUSD      int64
}

// LLMUsageRepository: SetDaily принимает накопленный итог дня, а не приращение.
type LLMUsageRepository interface {
	SetDaily(ctx context.Context, row *LLMUsageDaily) error
	ListDaily(ctx context.Context, from, to time.Time) ([]*LLMUsageDaily, error)
}
