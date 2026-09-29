package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/main-service/internal/platform/dbclient"
	"sync"
	"time"

	"github.com/example/main-service/internal/platform/contracts"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"github.com/example/main-service/internal/platform/logger"
)

// StartWorker запускает фиксированное число исполнителей; возвращённая функция отменяет и дожидается их.
func (s *HypothesisJobService) StartWorker(parent context.Context) func() {
	if s == nil || s.store == nil || s.hypotheses == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	for range backgroundJobConcurrency {
		wg.Go(func() { s.worker(ctx) })
	}
	return func() { cancel(); wg.Wait() }
}

func (s *HypothesisJobService) worker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		job, err := s.store.ClaimHypothesisJob(ctx)
		if err == nil && job != nil {
			s.run(ctx, job)
			continue
		}
		if err != nil && !errors.Is(err, dbclient.ErrNotFound) && ctx.Err() == nil {
			log := logger.From(ctx)
			log.Error().Err(err).Msg("claim hypothesis job failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

func (s *HypothesisJobService) run(parent context.Context, job *HypothesisJob) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	ctx = contracts.WithHypothesisLease(ctx, contracts.HypothesisLease{ID: job.ID, Token: job.Token})
	stopHeartbeat := s.startHeartbeat(ctx, job, cancel)
	defer stopHeartbeat()
	ids, err := s.execute(ctx, job.OwnerID, job.Kind, job.Input)
	if parent.Err() != nil {
		return
	}
	log := logger.From(ctx)
	if err != nil {
		job.Status = HypothesisJobFailed
		job.Error = "operation_failed"
		if errors.Is(err, ErrInsufficientContext) {
			job.Error = "insufficient_context"
		}
		log.Error().Err(err).Str("job_id", job.ID).Msg("hypothesis job failed")
	} else {
		job.Status = HypothesisJobSucceeded
		job.ResultIDs = ids
	}
	// Потеря ответа на финальную запись безопасна: тот же токен допускает её повтор.
	for attempt := range 3 {
		finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		saveErr := s.store.FinishHypothesisJob(finalCtx, job)
		finalCancel()
		if saveErr == nil {
			return
		}
		log.Error().Err(saveErr).Str("job_id", job.ID).Msg("save hypothesis job outcome failed")
		if attempt < 2 {
			timer := time.NewTimer(time.Second)
			select {
			case <-parent.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (s *HypothesisJobService) startHeartbeat(ctx context.Context, job *HypothesisJob, cancel context.CancelFunc) func() {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.store.RenewHypothesisJob(ctx, job.ID, job.Token); err != nil {
					log := logger.From(ctx)
					log.Error().Err(err).Str("job_id", job.ID).Msg("hypothesis job lease lost")
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(done); <-stopped }
}

func (s *HypothesisJobService) execute(
	ctx context.Context, ownerID, kind string, in HypothesisJobInput,
) ([]string, error) {
	switch kind {
	case HypothesisJobGenerate:
		kpi, err := s.jobKPI(ctx, ownerID, in)
		if err != nil {
			return nil, err
		}
		created, err := s.hypotheses.GenerateFromDocuments(ctx, ownerID, kpi, in.Count, in.Constraints, in.DocumentIDs)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(created))
		for _, h := range created {
			ids = append(ids, h.GetId())
		}
		return ids, nil
	case HypothesisJobVerify:
		return singleHypothesisID(s.hypotheses.Verify(ctx, ownerID, in.HypothesisID))
	case HypothesisJobEnrich:
		return singleHypothesisID(s.hypotheses.EnrichHypothesis(ctx, ownerID, in.HypothesisID))
	case HypothesisJobAssessTRL:
		return singleHypothesisID(s.hypotheses.AssessTRL(ctx, ownerID, in.HypothesisID))
	case HypothesisJobCompetitors:
		return singleHypothesisID(s.hypotheses.AnalyzeCompetitors(ctx, ownerID, in.HypothesisID))
	case HypothesisJobRefine:
		return singleHypothesisID(s.hypotheses.Refine(ctx, ownerID, in.HypothesisID))
	case HypothesisJobTag:
		return singleHypothesisID(s.hypotheses.TagHypothesis(ctx, ownerID, in.HypothesisID))
	default:
		return nil, fmt.Errorf("unsupported hypothesis job kind %q: %w", kind, ErrInvalidArgument)
	}
}

func (s *HypothesisJobService) jobKPI(
	ctx context.Context, ownerID string, in HypothesisJobInput,
) (*commonv1.KPI, error) {
	if in.KPIID != "" {
		return s.hypotheses.GetKPI(ctx, ownerID, in.KPIID)
	}
	return s.hypotheses.CreateKPI(ctx, ownerID, &dbv1.CreateKPIRequest{
		Title:       in.KPITitle,
		Metric:      in.KPIMetric,
		Description: in.KPIDescription,
	})
}

func singleHypothesisID(h *commonv1.Hypothesis, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	return []string{h.GetId()}, nil
}
