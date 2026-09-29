package application

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/dbclient"
	"sync"
	"time"
)

type fakeJobStore struct {
	mu        sync.Mutex
	jobs      map[string]*contracts.HypothesisJob
	order     []string
	claimGate chan struct{}
}

func newJobStore() *fakeJobStore { return &fakeJobStore{jobs: map[string]*contracts.HypothesisJob{}} }
func cloneJob(j *contracts.HypothesisJob) *contracts.HypothesisJob {
	c := *j
	c.ResultIDs = append([]string{}, j.ResultIDs...)
	c.Input.DocumentIDs = append([]string{}, j.Input.DocumentIDs...)
	return &c
}
func (f *fakeJobStore) EnqueueHypothesisJob(_ context.Context, j *contracts.HypothesisJob) (*contracts.HypothesisJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing := f.jobs[j.ID]; existing != nil {
		return cloneJob(existing), nil
	}
	c := cloneJob(j)
	c.Status = "queued"
	c.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	f.jobs[c.ID] = c
	f.order = append([]string{c.ID}, f.order...)
	return cloneJob(c), nil
}
func (f *fakeJobStore) GetHypothesisJob(_ context.Context, owner, id string) (*contracts.HypothesisJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	if j == nil || (owner != "" && owner != j.OwnerID) {
		return nil, dbclient.ErrNotFound
	}
	return cloneJob(j), nil
}
func (f *fakeJobStore) ListHypothesisJobs(_ context.Context, owner string, limit int) ([]*contracts.HypothesisJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*contracts.HypothesisJob{}
	for _, id := range f.order {
		j := f.jobs[id]
		if owner == "" || j.OwnerID == owner {
			out = append(out, cloneJob(j))
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (f *fakeJobStore) ClaimHypothesisJob(ctx context.Context) (*contracts.HypothesisJob, error) {
	f.mu.Lock()
	gate := f.claimGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.order {
		j := f.jobs[id]
		if j.Status == "queued" {
			j.Status = "running"
			j.Token = "lease"
			j.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return cloneJob(j), nil
		}
	}
	return nil, dbclient.ErrNotFound
}
func (f *fakeJobStore) RenewHypothesisJob(context.Context, string, string) error { return nil }
func (f *fakeJobStore) FinishHypothesisJob(_ context.Context, j *contracts.HypothesisJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[j.ID] = cloneJob(j)
	return nil
}
