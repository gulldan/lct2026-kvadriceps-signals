package application

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

type slowEnrichmentAnswerer struct {
	delay time.Duration
	calls int
}

func (a *slowEnrichmentAnswerer) Answer(ctx context.Context, _ *commonv1.RagRequest) (*commonv1.RagResponse, error) {
	a.calls++
	select {
	case <-time.After(a.delay):
		return &commonv1.RagResponse{Answer: enrichRich, Model: "test-model"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestEnrichmentBudgetKeepsSlowSuccessAndParentCancellation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		runtimeBudget int
		answerDelay   time.Duration
		parentBudget  time.Duration
		cancelAfter   time.Duration
		wantElapsed   time.Duration
		wantErr       error
	}{
		{name: "slow healthy answer", answerDelay: 110 * time.Second, wantElapsed: 110 * time.Second},
		{name: "configured budget expires", runtimeBudget: 120, answerDelay: 130 * time.Second,
			wantElapsed: 120 * time.Second, wantErr: context.DeadlineExceeded},
		{name: "shorter parent deadline", answerDelay: 110 * time.Second, parentBudget: 25 * time.Second,
			wantElapsed: 25 * time.Second, wantErr: context.DeadlineExceeded},
		{name: "parent cancellation", answerDelay: 110 * time.Second, cancelAfter: time.Second,
			wantElapsed: time.Second, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cat := newCatalog()
				cat.putHypothesis(&commonv1.Hypothesis{
					Id: "h1", OwnerId: "alice", Statement: "Aging raises yield strength.", Status: statusApproved,
					Detail: `{"existing":"keep"}`, Evidence: []*commonv1.HypothesisEvidence{{ChunkId: "c1"}},
				})
				settings := newRuntimeSettings()
				if tc.runtimeBudget > 0 {
					settings.settings["alice"] = &HypothesisRuntimeSettings{GenerationTimeoutSec: tc.runtimeBudget}
				}
				answerer := &slowEnrichmentAnswerer{delay: tc.answerDelay}
				svc := NewHypothesisService(cat, answerer, nil, settings, nil, nil, nil)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tc.parentBudget > 0 {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, tc.parentBudget)
					defer stop()
				}
				if tc.cancelAfter > 0 {
					time.AfterFunc(tc.cancelAfter, cancel)
				}
				started := time.Now()
				got, err := svc.EnrichHypothesis(ctx, "alice", "h1")
				if !errors.Is(err, tc.wantErr) || time.Since(started) != tc.wantElapsed || answerer.calls != 1 {
					t.Fatalf("enrichment budget: elapsed=%s calls=%d error=%v", time.Since(started), answerer.calls, err)
				}
				if tc.wantErr != nil {
					if got != nil || cat.updateCalls != 0 || cat.hypotheses["h1"].GetDetail() != `{"existing":"keep"}` {
						t.Fatal("canceled enrichment changed the saved hypothesis")
					}
				} else if cat.updateCalls != 1 || !enrichmentDone(got.GetAssessment()) {
					t.Fatal("slow successful enrichment was not persisted")
				}
			})
		})
	}
}
