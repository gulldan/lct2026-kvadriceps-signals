package application

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"
	"uuid"

	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/dbclient"
)

// Виды операций и состояния совпадают с долговечным протоколом БД.
const (
	HypothesisJobGenerate    = "generate"
	HypothesisJobVerify      = "verify"
	HypothesisJobEnrich      = "enrich"
	HypothesisJobAssessTRL   = "assess_trl"
	HypothesisJobCompetitors = "competitors"
	HypothesisJobRefine      = "refine"
	HypothesisJobTag         = "tag"
	HypothesisJobQueued      = "queued"
	HypothesisJobRunning     = "running"
	HypothesisJobSucceeded   = "succeeded"
	HypothesisJobFailed      = "failed"
	hypothesisJobListMax     = 200
	hypothesisJobGlobalMax   = 1000
	hypothesisJobActiveMax   = 2 * time.Minute
	hypothesisAutoVerifyTTL  = 30 * time.Minute
	hypothesisAutoTRLTTL     = 30 * time.Minute
	hypothesisAutoEnrichTTL  = 30 * time.Minute
	backgroundJobConcurrency = 2
)

type HypothesisJobInput = contracts.HypothesisJobInput

type HypothesisJob = contracts.HypothesisJob

type HypothesisJobStore = contracts.HypothesisJobStore

type HypothesisJobService struct {
	hypotheses *HypothesisService
	store      HypothesisJobStore
	wake       chan struct{}
}

func NewHypothesisJobService(hypotheses *HypothesisService, store HypothesisJobStore) *HypothesisJobService {
	return &HypothesisJobService{hypotheses: hypotheses, store: store, wake: make(chan struct{}, backgroundJobConcurrency)}
}

// Enqueue подтверждает приём только после долговечной записи намерения.
func (s *HypothesisJobService) Enqueue(
	ctx context.Context, ownerID, kind string, in HypothesisJobInput,
) (*HypothesisJob, error) {
	if s == nil || s.store == nil || s.hypotheses == nil {
		return nil, errors.New("hypothesis jobs unavailable")
	}
	if ownerID == "" {
		return nil, ErrForbidden
	}
	if len(in.RequestID) > 128 {
		return nil, ErrInvalidArgument
	}
	if err := validateHypothesisJobInput(kind, in); err != nil {
		return nil, err
	}
	id := uuid.NewV4().String()
	if in.RequestID != "" {
		id = contracts.StableID("hypothesis-job-v1", ownerID, in.RequestID)
	}
	job, err := s.store.EnqueueHypothesisJob(ctx, &HypothesisJob{
		ID: id, OwnerID: ownerID, Kind: kind, Input: in,
		Status: HypothesisJobQueued, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("enqueue hypothesis job: %w", err)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return job, nil
}

func validateHypothesisJobInput(kind string, in HypothesisJobInput) error {
	switch kind {
	case HypothesisJobGenerate:
		if in.KPIID == "" && in.KPITitle == "" {
			return fmt.Errorf("kpi_id or kpi_title is required: %w", ErrInvalidArgument)
		}
	case HypothesisJobVerify, HypothesisJobEnrich, HypothesisJobAssessTRL,
		HypothesisJobCompetitors, HypothesisJobRefine, HypothesisJobTag:
		if in.HypothesisID == "" {
			return fmt.Errorf("hypothesis_id is required: %w", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("unsupported hypothesis job kind %q: %w", kind, ErrInvalidArgument)
	}
	return nil
}

func (s *HypothesisJobService) Get(ctx context.Context, owner, id string) (*HypothesisJob, error) {
	job, err := s.store.GetHypothesisJob(ctx, owner, id)
	if errors.Is(err, dbclient.ErrNotFound) {
		return nil, ErrNotFound
	}
	return job, err
}

// GetAny читает без проверки владельца — только для административного пути.
func (s *HypothesisJobService) GetAny(ctx context.Context, id string) (*HypothesisJob, error) {
	if s == nil || s.store == nil {
		return nil, ErrNotFound
	}
	return s.store.GetHypothesisJob(ctx, "", id)
}

func (s *HypothesisJobService) List(ctx context.Context, owner string, limit int) ([]*HypothesisJob, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListHypothesisJobs(ctx, owner, min(limit, hypothesisJobListMax))
}

func (s *HypothesisJobService) ListRecentAll(ctx context.Context, limit int) []*HypothesisJob {
	if s == nil || s.store == nil {
		return nil
	}
	if limit <= 0 {
		limit = hypothesisJobListMax
	}
	jobs, err := s.store.ListHypothesisJobs(ctx, "", min(limit, hypothesisJobGlobalMax))
	if err != nil {
		return nil
	}
	return jobs
}

type HypothesisJobStat struct {
	Kind   string  `json:"kind"`
	Count  int     `json:"count"`
	P50Sec float64 `json:"p50_sec"`
	MaxSec float64 `json:"max_sec"`
}

func SummarizeHypothesisJobs(jobs []*HypothesisJob, now time.Time, window time.Duration) []HypothesisJobStat {
	durs := map[string][]float64{}
	for _, job := range jobs {
		if job == nil || job.Status != HypothesisJobSucceeded {
			continue
		}
		started, serr := time.Parse(time.RFC3339Nano, job.StartedAt)
		finished, ferr := time.Parse(time.RFC3339Nano, job.FinishedAt)
		if serr != nil || ferr != nil || finished.Before(started) || now.Sub(finished) > window {
			continue
		}
		durs[job.Kind] = append(durs[job.Kind], finished.Sub(started).Seconds())
	}
	out := make([]HypothesisJobStat, 0, len(durs))
	for _, kind := range slices.Sorted(maps.Keys(durs)) {
		d := durs[kind]
		slices.Sort(d)
		out = append(out, HypothesisJobStat{
			Kind:   kind,
			Count:  len(d),
			P50Sec: roundSec(d[(len(d)-1)/2]),
			MaxSec: roundSec(d[len(d)-1]),
		})
	}
	return out
}

func roundSec(v float64) float64 {
	return math.Round(v*1000) / 1000
}
