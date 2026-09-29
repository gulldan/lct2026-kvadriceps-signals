package application

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func jobService(t *testing.T, cat *fakeCatalog, a *scriptedAnswerer, store *fakeJobStore) *HypothesisJobService {
	t.Helper()
	svc := NewHypothesisJobService(newHypService(cat, a), store)
	t.Cleanup(svc.StartWorker(t.Context()))
	return svc
}

func waitJob(t *testing.T, svc *HypothesisJobService, id string) *HypothesisJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.Get(context.Background(), "alice", id)
		if err == nil && (job.Status == HypothesisJobSucceeded || job.Status == HypothesisJobFailed) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not reach a terminal status in time")
	return nil
}

func TestJobs_Enqueue_VerifyRunsToSuccess(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "alpha beta gamma delta"})
	verdict := reply(`{"verdict":"supported","confidence":0.9,"rationale":"",`+
		`"claims":[{"claim":"alpha beta gamma delta","verdict":"supported","quote":"alpha beta gamma delta"}],`+
		`"supporting":["alpha beta gamma delta"],"contradicting":[]}`,
		&commonv1.Source{ChunkId: "c1", Snippet: "alpha beta gamma delta epsilon"})
	a := newAnswerer(verdict)
	store := newJobStore()
	// Исполнитель ждёт, пока проверяется возвращённая запись: взяв задачу, он меняет тот же объект.
	gate := make(chan struct{})
	store.claimGate = gate
	svc := jobService(t, cat, a, store)

	job, err := svc.Enqueue(context.Background(), "alice", HypothesisJobVerify, HypothesisJobInput{HypothesisID: "h1"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.Status != HypothesisJobQueued {
		t.Fatalf("new job must be queued, got %q", job.Status)
	}
	close(gate)
	done := waitJob(t, svc, job.ID)
	if done.Status != HypothesisJobSucceeded {
		t.Fatalf("verify job should succeed, got %q (%s)", done.Status, done.Error)
	}
	if len(done.ResultIDs) != 1 || done.ResultIDs[0] != "h1" {
		t.Fatalf("succeeded job must carry the result id, got %v", done.ResultIDs)
	}
}

// Enqueue не должен делить возвращённую запись с исполнителем: HTTP-обработчик рендерит её, пока идёт run().
// Тест рендерит её всё время работы исполнителя, чтобы -race поймал общую запись.
func TestJobs_Enqueue_ReturnedJobIsRaceSafe(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "alpha beta gamma delta"})
	a := newAnswerer(reply(`{"verdict":"supported","confidence":0.9,"rationale":"",`+
		`"claims":[{"claim":"alpha beta gamma delta","verdict":"supported","quote":"alpha beta gamma delta"}],`+
		`"supporting":["alpha beta gamma delta"],"contradicting":[]}`,
		&commonv1.Source{ChunkId: "c1", Snippet: "alpha beta gamma delta epsilon"}))
	store := newJobStore() // без claimGate исполнитель стартует сразу
	svc := jobService(t, cat, a, store)

	job, err := svc.Enqueue(context.Background(), "alice", HypothesisJobVerify, HypothesisJobInput{HypothesisID: "h1"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if _, merr := json.Marshal(job); merr != nil {
					t.Errorf("marshal returned job: %v", merr)
					return
				}
			}
		}
	}()
	done := waitJob(t, svc, job.ID)
	close(stop)
	wg.Wait()

	if done.Status != HypothesisJobSucceeded {
		t.Fatalf("verify job should succeed, got %q (%s)", done.Status, done.Error)
	}
	if job.Status != HypothesisJobQueued {
		t.Fatalf("returned record must stay the queued snapshot, got %q", job.Status)
	}
}

func TestJobs_Enqueue_FailureRecorded(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	a := newAnswerer(failReply(errors.New("llm down")))
	svc := jobService(t, cat, a, newJobStore())
	job, err := svc.Enqueue(context.Background(), "alice", HypothesisJobVerify, HypothesisJobInput{HypothesisID: "h1"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	done := waitJob(t, svc, job.ID)
	if done.Status != HypothesisJobFailed || done.Error == "" {
		t.Fatalf("failed job must record an error, got %q / %q", done.Status, done.Error)
	}
}

func TestJobs_Enqueue_Validation(t *testing.T) {
	svc := jobService(t, newCatalog(), newAnswerer(reply("{}")), newJobStore())
	if _, err := svc.Enqueue(context.Background(), "", HypothesisJobVerify, HypothesisJobInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("blank owner must be forbidden, got %v", err)
	}
	_, err := svc.Enqueue(context.Background(), "alice", HypothesisJobVerify, HypothesisJobInput{})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing hypothesis_id must be invalid, got %v", err)
	}
	_, err = svc.Enqueue(context.Background(), "alice", "bogus", HypothesisJobInput{})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unknown kind must be invalid, got %v", err)
	}
	_, err = svc.Enqueue(context.Background(), "alice", HypothesisJobGenerate, HypothesisJobInput{})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("generate without kpi must be invalid, got %v", err)
	}
}

func TestJobs_Enqueue_Unconfigured(t *testing.T) {
	var svc *HypothesisJobService
	if _, err := svc.Enqueue(context.Background(), "alice", HypothesisJobVerify, HypothesisJobInput{HypothesisID: "h"}); err == nil {
		t.Fatal("nil service must refuse to enqueue")
	}
}

func TestJobs_Get_NotFound(t *testing.T) {
	svc := jobService(t, newCatalog(), newAnswerer(reply("{}")), newJobStore())
	if _, err := svc.Get(context.Background(), "alice", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown job must be not-found, got %v", err)
	}
}

func TestJobs_List(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	svc := jobService(t, cat, newAnswerer(failReply(errors.New("x"))), newJobStore())
	job, err := svc.Enqueue(context.Background(), "alice", HypothesisJobVerify, HypothesisJobInput{HypothesisID: "h1"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitJob(t, svc, job.ID)
	jobs, err := svc.List(context.Background(), "alice", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("List must return the enqueued job, got %v", jobs)
	}
}

func TestSummarizeHypothesisJobs(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	mk := func(kind, status string, start, finish time.Duration) *HypothesisJob {
		return &HypothesisJob{Kind: kind, Status: status, StartedAt: ts(start), FinishedAt: ts(finish)}
	}
	jobs := []*HypothesisJob{
		mk(HypothesisJobGenerate, HypothesisJobSucceeded, -10*time.Minute, -9*time.Minute),  // 60 с
		mk(HypothesisJobGenerate, HypothesisJobSucceeded, -20*time.Minute, -17*time.Minute), // 180 с
		mk(HypothesisJobGenerate, HypothesisJobSucceeded, -30*time.Minute, -28*time.Minute), // 120 с
		mk(HypothesisJobVerify, HypothesisJobSucceeded, -5*time.Minute, -5*time.Minute+30*time.Second),
		mk(HypothesisJobVerify, HypothesisJobFailed, -5*time.Minute, -4*time.Minute),    // провалена
		mk(HypothesisJobEnrich, HypothesisJobSucceeded, -30*time.Hour, -25*time.Hour),   // вне окна
		mk(HypothesisJobEnrich, HypothesisJobSucceeded, -2*time.Minute, -3*time.Minute), // конец раньше начала
		{Kind: HypothesisJobTag, Status: HypothesisJobSucceeded, StartedAt: "bad", FinishedAt: ts(0)},
		nil,
	}
	got := SummarizeHypothesisJobs(jobs, now, 24*time.Hour)
	want := []HypothesisJobStat{
		{Kind: HypothesisJobGenerate, Count: 3, P50Sec: 120, MaxSec: 180},
		{Kind: HypothesisJobVerify, Count: 1, P50Sec: 30, MaxSec: 30},
	}
	if len(got) != len(want) {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stats[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if s := SummarizeHypothesisJobs(nil, now, time.Hour); len(s) != 0 {
		t.Fatalf("empty input must yield no stats, got %+v", s)
	}
}
