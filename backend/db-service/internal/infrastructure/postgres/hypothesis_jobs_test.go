package postgres_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/contracts"
)

func TestHypothesisJobsRecoverLeaseWithoutDuplicatingEffects(t *testing.T) {
	db := newTestDB(t)
	jobs := postgres.NewHypothesisJobsRepo(db)
	ctx := t.Context()
	in := &contracts.HypothesisJob{
		ID:      uuid.NewV4().String(),
		OwnerID: uuid.NewV4().String(),
		Kind:    "generate",
		Input: contracts.HypothesisJobInput{
			KPITitle: "target",
		},
	}
	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM hypotheses WHERE owner_id=$1`,
			`DELETE FROM kpis WHERE owner_id=$1`,
			`DELETE FROM hypothesis_job_effects WHERE job_id IN(SELECT id FROM hypothesis_jobs WHERE owner_id=$1)`,
			`DELETE FROM hypothesis_jobs WHERE owner_id=$1`,
		} {
			if _, err := db.Pool.Exec(context.Background(), query, in.OwnerID); err != nil {
				t.Error(err)
			}
		}
	})
	queued, err := jobs.EnqueueHypothesisJob(ctx, in)
	if err != nil || queued.Status != "queued" {
		t.Fatalf("enqueue: %+v %v", queued, err)
	}
	if _, err = jobs.EnqueueHypothesisJob(ctx, in); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	var first *contracts.HypothesisJob
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			j, e := jobs.ClaimHypothesisJob(ctx)
			if e != nil && !errors.Is(e, domain.ErrNotFound) {
				t.Error(e)
			}
			if j != nil {
				claims.Add(1)
				first = j
			}
		})
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claim winners=%d", claims.Load())
	}
	oldCtx := contracts.WithHypothesisLease(ctx, contracts.HypothesisLease{ID: first.ID, Token: first.Token})
	kpiRepo := postgres.NewKPIRepo(db)
	kpi := &domain.KPI{
		ID:        uuid.NewV4().String(),
		OwnerID:   in.OwnerID,
		Title:     "first",
		Direction: "increase",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err = kpiRepo.Create(oldCtx, kpi); err != nil {
		t.Fatal(err)
	}
	// Процесс остановился после прикладной фиксации до сохранения исхода задания.
	if _, err = db.Pool.Exec(
		ctx,
		`UPDATE hypothesis_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`,
		in.ID,
	); err != nil {
		t.Fatal(err)
	}
	second, err := jobs.ClaimHypothesisJob(ctx)
	if err != nil || second == nil || second.Attempt != 2 || second.Token == first.Token {
		t.Fatalf("recover: %+v %v", second, err)
	}
	replay := *kpi
	replay.ID = uuid.NewV4().String()
	replay.Title = "regenerated"
	if err = kpiRepo.Create(oldCtx, &replay); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale publish: %v", err)
	}
	newCtx := contracts.WithHypothesisLease(ctx, contracts.HypothesisLease{ID: second.ID, Token: second.Token})
	if err = kpiRepo.Create(newCtx, &replay); err != nil || replay.ID != kpi.ID || replay.Title != "first" {
		t.Fatalf("effect replay: %+v %v", replay, err)
	}
	if err = jobs.RenewHypothesisJob(ctx, first.ID, first.Token); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale renewal: %v", err)
	}
	repo := postgres.NewHypothesisRepo(db)
	hyp := &domain.Hypothesis{
		ID:        uuid.NewV4().String(),
		OwnerID:   in.OwnerID,
		Title:     "hyp",
		Statement: "original",
		Method:    "manual",
		Status:    "generated",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err = repo.Create(newCtx, hyp, nil); err != nil {
		t.Fatal(err)
	}
	repeat := *hyp
	repeat.ID = uuid.NewV4().String()
	repeat.Statement = "different"
	if err = repo.Create(newCtx, &repeat, nil); err != nil || repeat.ID != hyp.ID || repeat.Statement != "original" {
		t.Fatalf("hyp replay: %+v %v", repeat, err)
	}
	hyp.Statement = "updated"
	if err = repo.Update(newCtx, hyp, nil); err != nil {
		t.Fatal(err)
	}
	version := hyp.Version
	hyp.Statement = "again"
	if err = repo.Update(newCtx, hyp, nil); err != nil || hyp.Version != version || hyp.Statement != "updated" {
		t.Fatalf("update replay: %+v %v", hyp, err)
	}
	first.Status = "succeeded"
	if err = jobs.FinishHypothesisJob(ctx, first); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale final: %v", err)
	}
	second.Status = "succeeded"
	second.ResultIDs = []string{hyp.ID}
	for range 2 {
		if err = jobs.FinishHypothesisJob(ctx, second); err != nil {
			t.Fatal(err)
		}
	}
	got, err := jobs.GetHypothesisJob(ctx, in.OwnerID, in.ID)
	if err != nil || got.Status != "succeeded" || len(got.ResultIDs) != 1 {
		t.Fatalf("final outcome: %+v %v", got, err)
	}
	if _, err = jobs.GetHypothesisJob(ctx, "foreign", in.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("owner isolation: %v", err)
	}
}
