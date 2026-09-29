package application

import (
	"context"
	"time"
)

// Явный дедлайн доходит через gRPC и освобождает слот допуска в llm-service: таймаут записи HTTP сам RPC
// не отменяет. Бюджеты — по числу последовательных проходов LLM, p95 ≈ 27 с на проход.
const (
	llmSinglePassTimeout = 90 * time.Second
	// Запись результата проверки получает отдельный короткий бюджет после проходов модели.
	verifyPersistTimeout = 10 * time.Second
	// llmRefineTimeout: проверка → правка → перепроверка, до ~4 проходов.
	llmRefineTimeout = 200 * time.Second
)

func withDeadline(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= d {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
