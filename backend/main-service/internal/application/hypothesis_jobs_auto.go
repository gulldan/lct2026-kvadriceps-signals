package application

import (
	"context"
	"strconv"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func hypothesisAutoVerifyKey(ownerID, hypothesisID string) string {
	return "rag:hypothesis_jobs:auto_verify:" + ownerID + ":" + hypothesisID
}

func hypothesisAutoTRLKey(ownerID, hypothesisID string) string {
	return "rag:hypothesis_jobs:auto_trl:" + ownerID + ":" + hypothesisID
}

func hypothesisAutoEnrichKey(ownerID, hypothesisID string) string {
	return "rag:hypothesis_jobs:auto_enrich:" + ownerID + ":" + hypothesisID
}

func (s *HypothesisJobService) activeKindCount(ctx context.Context, ownerID, kind string) int {
	jobs, err := s.List(ctx, ownerID, hypothesisJobListMax)
	if err != nil {
		return 0
	}
	active := 0
	for _, job := range jobs {
		if job == nil || job.Kind != kind {
			continue
		}
		if hypothesisJobCountsAsActive(job) {
			active++
		}
	}
	return active
}

func hypothesisJobCountsAsActive(job *HypothesisJob) bool {
	if job.Status == HypothesisJobQueued {
		return true
	}
	if job.Status != HypothesisJobRunning {
		return false
	}
	heartbeat := job.HeartbeatAt
	if heartbeat == "" {
		heartbeat = job.StartedAt
	}
	t, err := time.Parse(time.RFC3339Nano, heartbeat)
	return err == nil && time.Since(t) <= hypothesisJobActiveMax
}

// ensureAutoJobs: requestID с окном времени markerTTL делает постановку идемпотентной, чтобы пассивные
// обновления страницы не заваливали поиск и модель дублями.
func (s *HypothesisJobService) ensureAutoJobs(
	ctx context.Context,
	ownerID string,
	hs []*commonv1.Hypothesis,
	limit int,
	kind string,
	eligible func(*commonv1.Hypothesis, string) bool,
	markerKey func(ownerID, hypothesisID string) string,
	markerTTL time.Duration,
) int {
	if s == nil || s.store == nil || s.hypotheses == nil || ownerID == "" || limit == 0 {
		return 0
	}
	if limit < 0 {
		limit = len(hs)
	}
	capacity := limit
	if limit > 0 {
		capacity -= s.activeKindCount(ctx, ownerID, kind)
		if capacity <= 0 {
			return 0
		}
	}
	started := 0
	for _, h := range hs {
		if !eligible(h, ownerID) {
			continue
		}

		requestID := markerKey(ownerID, h.GetId()) + ":" + strconv.FormatInt(time.Now().Unix()/int64(markerTTL/time.Second), 10)
		if _, err := s.Enqueue(ctx, ownerID, kind, HypothesisJobInput{HypothesisID: h.GetId(), RequestID: requestID}); err != nil {
			continue
		}

		started++
		if capacity > 0 && started >= capacity {
			break
		}
	}
	return started
}

func (s *HypothesisJobService) EnsureVerify(
	ctx context.Context,
	ownerID string,
	hs []*commonv1.Hypothesis,
	limit int,
) int {
	return s.ensureAutoJobs(
		ctx, ownerID, hs, limit,
		HypothesisJobVerify, verifyEligible, hypothesisAutoVerifyKey, hypothesisAutoVerifyTTL,
	)
}

func (s *HypothesisJobService) EnsureAssessTRL(
	ctx context.Context,
	ownerID string,
	hs []*commonv1.Hypothesis,
	limit int,
) int {
	return s.ensureAutoJobs(
		ctx, ownerID, hs, limit,
		HypothesisJobAssessTRL, assessTRLEligible, hypothesisAutoTRLKey, hypothesisAutoTRLTTL,
	)
}

func (s *HypothesisJobService) EnsureEnrich(
	ctx context.Context,
	ownerID string,
	hs []*commonv1.Hypothesis,
	limit int,
) int {
	return s.ensureAutoJobs(
		ctx, ownerID, hs, limit,
		HypothesisJobEnrich, enrichEligible, hypothesisAutoEnrichKey, hypothesisAutoEnrichTTL,
	)
}

// enrichEligible: обогащение идёт строго после проверки — вердикт корпуса уже должен быть.
func enrichEligible(h *commonv1.Hypothesis, ownerID string) bool {
	if h == nil || h.GetOwnerId() != ownerID || h.GetId() == "" {
		return false
	}
	if h.GetStatus() == statusRejected || h.GetStatus() == statusArchived {
		return false
	}
	return verdictFromAssessment(h.GetAssessment()) != "" && !enrichmentDone(h.GetAssessment())
}

func verifyEligible(h *commonv1.Hypothesis, ownerID string) bool {
	if h == nil || h.GetOwnerId() != ownerID || h.GetId() == "" {
		return false
	}
	if h.GetStatus() == statusRejected || h.GetStatus() == statusArchived {
		return false
	}
	return verdictFromAssessment(h.GetAssessment()) == ""
}

// assessTRLEligible: оценка УГТ тоже идёт строго после проверки.
func assessTRLEligible(h *commonv1.Hypothesis, ownerID string) bool {
	if h == nil || h.Trl != nil || h.GetOwnerId() != ownerID || h.GetId() == "" {
		return false
	}
	if h.GetStatus() == statusRejected || h.GetStatus() == statusArchived {
		return false
	}
	return verdictFromAssessment(h.GetAssessment()) != ""
}
