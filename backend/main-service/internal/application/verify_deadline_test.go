package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/contracts"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"google.golang.org/protobuf/proto"
)

type verifyBudgetAnswerer struct {
	delays  []time.Duration
	mainErr error
	calls   int
}

func (a *verifyBudgetAnswerer) Answer(ctx context.Context, _ *commonv1.RagRequest) (*commonv1.RagResponse, error) {
	i := a.calls
	a.calls++
	select {
	case <-time.After(a.delays[min(i, len(a.delays)-1)]):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if i == 0 && a.mainErr != nil {
		return nil, a.mainErr
	}
	if i == 2 {
		return &commonv1.RagResponse{Answer: `{"query":"material strength"}`}, nil
	}
	return &commonv1.RagResponse{
		Answer: `{"verdict":"insufficient","confidence":0.8,"rationale":"Main analysis completed"}`,
		Sources: []*commonv1.Source{{
			DocumentId: "d1", ChunkId: "c1", Filename: "paper.pdf", Snippet: "Context without evidence for the effect.",
		}},
	}, nil
}

type verifyBudgetCatalog struct {
	*fakeCatalog
	attempts  int
	remaining time.Duration
	lease     contracts.HypothesisLease
}

func (c *verifyBudgetCatalog) GetHypothesis(ctx context.Context, id string) (*commonv1.Hypothesis, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h, err := c.fakeCatalog.GetHypothesis(ctx, id)
	return proto.CloneOf(h), err
}

func (c *verifyBudgetCatalog) UpdateHypothesis(ctx context.Context, req *dbv1.UpdateHypothesisRequest) error {
	c.attempts++
	deadline, _ := ctx.Deadline()
	c.remaining = time.Until(deadline)
	c.lease = contracts.HypothesisLeaseFrom(ctx)
	select {
	case <-time.After(time.Second):
		return c.fakeCatalog.UpdateHypothesis(ctx, req)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestVerifyBudgetPreservesMainResultAndParentCancellation(t *testing.T) {
	mainErr := errors.New("main model unavailable")
	for _, tc := range []struct {
		name          string
		delays        []time.Duration
		runtimeBudget int
		parentBudget  time.Duration
		cancelAfter   time.Duration
		mainErr       error
		wantErr       error
		wantElapsed   time.Duration
		wantCalls     int
	}{
		{name: "slow main then counter timeout", delays: []time.Duration{160 * time.Second, 6 * time.Minute},
			wantElapsed: 301 * time.Second, wantCalls: 2},
		{name: "publication rejudge timeout", delays: []time.Duration{80 * time.Second, 20 * time.Second,
			time.Second, 6 * time.Minute}, wantElapsed: 301 * time.Second, wantCalls: 4},
		{name: "configured budget", runtimeBudget: 60, delays: []time.Duration{time.Second, 6 * time.Minute},
			wantElapsed: 61 * time.Second, wantCalls: 2},
		{name: "parent deadline", parentBudget: 25 * time.Second, delays: []time.Duration{time.Second, 6 * time.Minute},
			wantElapsed: 25 * time.Second, wantCalls: 2, wantErr: context.DeadlineExceeded},
		{name: "parent cancellation", cancelAfter: 2 * time.Second, delays: []time.Duration{time.Second, 6 * time.Minute},
			wantElapsed: 2 * time.Second, wantCalls: 2, wantErr: context.Canceled},
		{name: "main failure", mainErr: mainErr, delays: []time.Duration{time.Second},
			wantElapsed: time.Second, wantCalls: 1, wantErr: mainErr},
		{name: "main deadline", delays: []time.Duration{6 * time.Minute},
			wantElapsed: 300 * time.Second, wantCalls: 1, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cat := &verifyBudgetCatalog{fakeCatalog: newCatalog()}
				cat.putHypothesis(&commonv1.Hypothesis{
					Id: "h1", OwnerId: "alice", Statement: "The effect remains unverified", Assessment: `{"existing":"keep"}`,
				})
				settings := newRuntimeSettings()
				settings.settings["alice"] = &HypothesisRuntimeSettings{GenerationTimeoutSec: tc.runtimeBudget}
				answerer := &verifyBudgetAnswerer{delays: tc.delays, mainErr: tc.mainErr}
				svc := NewHypothesisService(cat, answerer, nil, settings, nil, nil, nil)
				svc.SetPubSearcher(&fakePubs{works: []ExternalWork{{Title: "Study", Abstract: "Unverified effect"}}})
				lease := contracts.HypothesisLease{ID: "verify-job", Token: "active-lease"}
				ctx, cancel := context.WithCancel(contracts.WithHypothesisLease(t.Context(), lease))
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
				got, err := svc.Verify(ctx, "alice", "h1")
				if !errors.Is(err, tc.wantErr) || time.Since(started) != tc.wantElapsed || answerer.calls != tc.wantCalls {
					t.Fatalf("verify budget: elapsed=%s calls=%d error=%v", time.Since(started), answerer.calls, err)
				}
				if tc.wantErr != nil {
					if got != nil || cat.attempts != 0 || cat.hypotheses["h1"].GetAssessment() != `{"existing":"keep"}` {
						t.Fatal("failed or canceled verification attempted to persist a result")
					}
					return
				}
				if cat.updateCalls != 1 || cat.remaining <= 0 || cat.remaining > 10*time.Second || cat.lease != lease {
					t.Fatalf("unsafe persistence budget/lease: calls=%d remaining=%s lease=%+v", cat.updateCalls, cat.remaining, cat.lease)
				}
				if checkVerdict(got.GetAssessment()) != verdictInsufficient || len(got.GetEvidence()) != 0 ||
					!strings.Contains(got.GetAssessment(), "Main analysis completed") {
					t.Fatalf("optional timeout lost or fabricated main verification: %s", got.GetAssessment())
				}
			})
		})
	}
}
