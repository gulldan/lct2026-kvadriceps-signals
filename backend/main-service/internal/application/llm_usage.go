package application

import (
	"context"
	"time"

	"google.golang.org/grpc/metadata"

	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"github.com/example/main-service/internal/platform/llmusage"
)

const dayFmt = "2006-01-02"

// opCtx помечает вызов операцией, чтобы llm-service отнёс расход токенов и стоимость к ней.
func opCtx(ctx context.Context, operation string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "operation", operation)
}

type LLMUsageDB interface {
	SetLLMUsageDaily(
		ctx context.Context, day, model, operation string,
		requests, promptTokens, completionTokens, costNanoUSD int64,
	) error
	ListLLMUsageDaily(ctx context.Context, fromDay, toDay string) ([]*dbv1.LLMUsageDailyRow, error)
}

// LLMUsageService переносит дневные счётчики Valkey (их пишут llm-service, chunk-splitter и Python-воркеры)
// в долговечный журнал Postgres.
type LLMUsageService struct {
	kv llmusage.KV
	db LLMUsageDB
}

func NewLLMUsageService(kv llmusage.KV, db LLMUsageDB) *LLMUsageService {
	return &LLMUsageService{kv: kv, db: db}
}

// Flush идемпотентен: значения перезаписываются, а не прибавляются; ошибки намеренно пропускаются.
func (s *LLMUsageService) Flush(ctx context.Context) {
	if s == nil || s.kv == nil || s.db == nil {
		return
	}
	now := time.Now().UTC()
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		cells, err := llmusage.ReadDay(ctx, s.kv, day)
		if err != nil {
			continue
		}
		ds := day.Format(dayFmt)
		for _, c := range cells {
			_ = s.db.SetLLMUsageDaily(ctx, ds, c.Model, c.Operation,
				c.Requests, c.PromptTokens, c.CompletionTokens, c.CostNanoUSD)
		}
	}
}

func (s *LLMUsageService) List(ctx context.Context, from, to time.Time) ([]*dbv1.LLMUsageDailyRow, error) {
	s.Flush(ctx)
	return s.db.ListLLMUsageDaily(ctx, from.Format(dayFmt), to.Format(dayFmt))
}

// TodayRequests читает горячий хеш: он свежее перенесённого журнала.
func (s *LLMUsageService) TodayRequests(ctx context.Context) int64 {
	if s == nil || s.kv == nil {
		return 0
	}
	cells, err := llmusage.ReadDay(ctx, s.kv, time.Now().UTC())
	if err != nil {
		return 0
	}
	var n int64
	for _, c := range cells {
		n += c.Requests
	}
	return n
}

func (s *LLMUsageService) ReqPerMinute(ctx context.Context) int64 {
	if s == nil || s.kv == nil {
		return 0
	}
	return llmusage.ReqPerMinute(ctx, s.kv)
}
