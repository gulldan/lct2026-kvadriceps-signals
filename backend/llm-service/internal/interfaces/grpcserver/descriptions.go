package grpcserver

import (
	"context"
	"time"

	"github.com/example/llm-service/internal/platform/analysisdoc"
	"github.com/example/llm-service/internal/platform/logger"
)

// DescriptionStore не принимает произвольный контекст от вызывающего и не пишет в опубликованный числовой результат.
type DescriptionStore interface {
	ClaimDescription(context.Context, analysisdoc.DescriptionClaim) (analysisdoc.DescriptionClaimed, error)
	CompleteDescription(context.Context, analysisdoc.DescriptionComplete) (analysisdoc.DescriptionJob, error)
}

type DescriptionGenerator interface {
	Describe(context.Context, analysisdoc.FactBundle) analysisdoc.DescriptionResult
}

// RunDescriptions делит с чатом слоты и лимит RPM модели;
// при остановке в БД остаётся лиза, которую подберут позже.
func (s *Server) RunDescriptions(ctx context.Context, store DescriptionStore, generator DescriptionGenerator) {
	for ctx.Err() == nil {
		worked := s.descriptionAttempt(ctx, store, generator)
		if worked {
			continue
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Server) descriptionAttempt(ctx context.Context, store DescriptionStore, generator DescriptionGenerator) bool {
	// Описания не встают за слотом впереди интерактивных запросов.
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return false
	}
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	claim, err := store.ClaimDescription(claimCtx, analysisdoc.DescriptionClaim{Version: analysisdoc.DescriptionVersion})
	cancel()
	if err != nil || claim.Bundle == nil || claim.Lease == nil {
		return false
	}
	result := generator.Describe(ctx, *claim.Bundle)
	if ctx.Err() != nil {
		return false
	}
	saveCtx, saveCancel := context.WithTimeout(ctx, 10*time.Second)
	defer saveCancel()
	_, err = store.CompleteDescription(saveCtx, analysisdoc.DescriptionComplete{DescriptionLease: *claim.Lease, Result: result})
	if err != nil {
		logger.From(ctx).Warn().Str("job_id", claim.Lease.JobID).Msg("description completion deferred; lease will recover")
		return false
	}
	return true
}
