package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestSearchProgressPreservesVisitsAndWaitDeadline(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	now := time.Date(2026, time.September, 27, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	job := s.newSearchJob("energy", "owner", nil)
	s.searchJobs[liveSearchKey("owner", "energy")] = job
	job.noteProgress(liveStepArxiv, 30*time.Second, now)
	first, ok := s.SearchState("energy", "owner")
	if !ok || first.WaitSeconds != 30 || !slices.Equal(first.VisitedSteps, []string{liveStepArxiv}) {
		t.Fatalf("first progress: %+v", first)
	}
	now = now.Add(7*time.Second + 100*time.Millisecond)
	poll, ok := s.currentSearch(t.Context(), liveSearchKey("owner", "energy"), "energy")
	if !ok || poll.WaitSeconds != 23 || poll.ElapsedSeconds != 7 {
		t.Fatalf("poll reset the source pause: %+v", poll)
	}
	now = now.Add(24 * time.Second)
	poll, _ = s.SearchState("energy", "owner")
	if poll.WaitSeconds != 0 {
		t.Fatalf("expired pause: %+v", poll)
	}
	// Возврат к патентам после новостей не должен стирать историю или отмечать пропущенные журналы.
	for _, step := range []string{liveStepPatents, liveStepWeb, liveStepPatents} {
		job.noteProgress(step, 0, now)
	}
	poll, _ = s.SearchState("energy", "owner")
	if poll.Step != liveStepPatents || poll.Index != liveStepIndex(liveStepPatents) ||
		!slices.Equal(poll.VisitedSteps, []string{liveStepArxiv, liveStepPatents, liveStepWeb}) {
		t.Fatalf("visits were inferred from the stage order: %+v", poll)
	}
	if !slices.Equal(first.VisitedSteps, []string{liveStepArxiv}) || first.WaitSeconds != 30 {
		t.Fatalf("a later update mutated an earlier response: %+v", first)
	}
}

func TestSearchPollReturnsFailureWithoutRestarting(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	s.snapshot = &LiveSnapshot{}
	// Настоящий работник падает сразу, без обращения к провайдеру.
	s.searchSlot <- struct{}{}
	s.slotWait = time.Millisecond
	state, err := s.StartSearch(t.Context(), "blocked", "owner", false)
	if err != nil || state.Status != LiveSearchRunning {
		t.Fatalf("start: %+v %v", state, err)
	}
	s.mu.RLock()
	job := s.searchJobs[liveSearchKey("owner", "blocked")]
	s.mu.RUnlock()
	<-job.done
	for range 10 {
		state, err = s.StartSearch(t.Context(), "blocked", "owner", false)
		if err != nil || state.Status != LiveSearchFailed || state.Error != ErrLiveSearchBusy.Error() {
			t.Fatalf("poll restarted a failed search: %+v %v", state, err)
		}
	}
	if s.searchJobs[liveSearchKey("owner", "blocked")] != job {
		t.Fatal("poll replaced the saved attempt")
	}
}

func TestSearchAdmissionRefusesAnotherQueryImmediately(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	s.snapshot = &LiveSnapshot{}
	s.searchJobs[liveSearchKey("owner", "active")] = &liveSearchJob{
		started: time.Now(), state: LiveSearchState{Status: LiveSearchRunning}}
	started := time.Now()
	if _, err := s.StartSearch(t.Context(), "other", "owner", false); !errors.Is(err, ErrLiveSearchBusy) {
		t.Fatalf("a second query joined a queue: %v", err)
	}
	if time.Since(started) > 100*time.Millisecond || len(s.searchJobs) != 1 {
		t.Fatal("admission waited or created another job")
	}
	if state, err := s.StartSearch(t.Context(), "active", "owner", false); err != nil || state.Status != LiveSearchRunning {
		t.Fatalf("polling the active query was refused: %+v %v", state, err)
	}
}

func TestPersonSearchPreemptsBackgroundWork(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	s.snapshot = &LiveSnapshot{}
	refreshed, turned := false, false
	s.searchJobs[liveSearchKey("", "nightly")] = &liveSearchJob{started: time.Now(), background: true,
		cancel: func() { refreshed = true }, state: LiveSearchState{Status: LiveSearchRunning}}
	s.background[1] = func() { turned = true }
	s.mu.Lock()
	err := s.admitSearch(false)
	s.mu.Unlock()
	if err != nil || !refreshed || !turned {
		t.Fatalf("person search must preempt background work: %v refresh=%v turn=%v", err, refreshed, turned)
	}
	s.searchJobs[liveSearchKey("owner", "mine")] = &liveSearchJob{started: time.Now(),
		state: LiveSearchState{Status: LiveSearchRunning}}
	s.mu.Lock()
	busy := s.admitSearch(true)
	s.mu.Unlock()
	if !errors.Is(busy, ErrLiveSearchBusy) {
		t.Fatalf("nightly refresh must wait for a person search: %v", busy)
	}
}

func TestReopenGivesPreviousScoreWhileRescoring(t *testing.T) {
	s := NewTrendLiveService(nil, nil)
	s.SetSearchStore(newFakeSearchStore())
	s.snapshot = &LiveSnapshot{}
	s.reopenPatience = 20 * time.Millisecond
	old := LiveModelScore{Label: liveClassifierWeak, Value: 0.6, Threshold: 0.3}
	candidate := storedCandidateFixture("c1")
	candidate.ModelScore = &old
	s.saveSearch(t.Context(), "запрос", liveScopedDetailFixture("запрос",
		LiveDetail{Candidates: []LiveTopic{candidate}}), time.Now().Add(time.Hour))
	if _, ok := s.storedSearch(t.Context(), "запрос", nil); !ok {
		t.Fatal("исходный проверенный ответ не сохранён для повторного открытия")
	}
	release := make(chan struct{})
	s.SetClassifier(func(ctx context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return LiveClassifierResponse{}, ctx.Err()
		}
		return LiveClassifierResponse{FeatureSchema: LiveClassifierSchema, Predictions: []LiveClassifierPrediction{{
			ID: "c1", LiveModelScore: LiveModelScore{Label: liveClassifierWeak, Value: 0.9, Threshold: 0.3,
				Model: "m", Version: input.ModelVersion, FeatureSchema: LiveClassifierSchema,
				Basis: "Проверочная оценка модели", Features: []LiveModelFeature{{Name: "Рост доли", Value: "1",
					Contribution: 0.5}}}}}}, nil
	}, "new")
	poll := func(want func(LiveDetail) bool) LiveDetail {
		t.Helper()
		for range 200 {
			state, err := s.StartSearch(t.Context(), "запрос", "owner", false)
			if err != nil {
				t.Fatal(err)
			}
			if state.Status == LiveSearchReady && want(*state.Detail) {
				return *state.Detail
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("ответ не пришёл")
		return LiveDetail{}
	}
	first := poll(func(LiveDetail) bool { return true })
	if !first.Rescoring || first.Candidates[0].ModelScore == nil || first.Candidates[0].ModelScore.Value != 0.6 {
		t.Fatalf("ответ ждал дооценку или потерял прежнюю оценку: %+v", first)
	}
	close(release)
	scored := poll(func(detail LiveDetail) bool { return !detail.Rescoring })
	if scored.ClassifierVersion != "new" || scored.Candidates[0].ModelScore.Value != 0.9 {
		t.Fatalf("новая оценка не подставлена: %+v", scored)
	}
}
