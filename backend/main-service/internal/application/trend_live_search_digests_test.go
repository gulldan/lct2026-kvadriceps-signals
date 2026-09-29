package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Обычный опрос не запускает новый сбор, пока частичный ответ ждёт уже начатые пересказы.
func TestFreshSearchDeliversDigestsAfterModelFailure(t *testing.T) {
	for _, mode := range []string{"model-failure", "shortfall", "digest-timeout", "partial-timeout", "refresh"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) { testFreshSearchDigests(t, mode) })
		})
	}
}

func testFreshSearchDigests(t *testing.T, mode string) {
	t.Helper()
	// Отрицательный контроль тоже дожидается уже начатых фоновых задач перед выходом из виртуального времени.
	defer func() {
		time.Sleep(liveLateDigestWait)
		synctest.Wait()
	}()
	const query = "phase coherence"
	service, collections := liveRetryServiceLanguage(t, "en")
	service.SetWorkers(func(context.Context, string, int) int { return 1 })
	service.SetExtractor(func(context.Context, CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
		return []LiveExtractedTechnology{
			{Name: "Phase coherence spectrometer", Documents: []string{"fixture:0"}},
			{Name: "Synthetic speech detector", Documents: []string{"fixture:1"}},
		}, nil
	})
	service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
		for _, candidate := range input.Candidates {
			if mode != "shortfall" && strings.Contains(candidate.ID, "synthetic-speech-detector") {
				return output, errors.New("временный отказ второй модельной оценки")
			}
			output.Predictions = append(output.Predictions, LiveClassifierPrediction{ID: candidate.ID,
				LiveModelScore: LiveModelScore{Label: liveClassifierWeak, Value: .9, Threshold: .75,
					Model: "fixture", Version: input.ModelVersion, FeatureSchema: LiveClassifierSchema,
					Basis: "Проверка жизненного цикла", Features: []LiveModelFeature{{Name: "Рост", Value: "1"}}}})
		}
		return output, nil
	}, "fixture-model")
	store := newFakeSearchStore()
	service.SetSearchStore(store)
	var digests atomic.Int32
	service.digest = func(ctx context.Context, _, _ string) (string, string, error) {
		call := digests.Add(1)
		if mode == "digest-timeout" || mode == "partial-timeout" && call > 1 {
			<-ctx.Done()
			if mode == "partial-timeout" {
				time.Sleep(time.Second)
			}
			return "", "", ctx.Err()
		}
		wait := 30 * time.Second
		if mode == "refresh" && call > 3 {
			wait = time.Minute
		}
		time.Sleep(wait)
		return "Пересказ исходного эксперимента.", "digest-model", nil
	}
	key := liveSearchKey("owner", query)
	state, err := service.StartSearch(t.Context(), query, "owner", false)
	if err != nil || state.Status != LiveSearchRunning {
		t.Fatalf("start: %+v %v", state, err)
	}
	service.mu.RLock()
	job := service.searchJobs[key]
	service.mu.RUnlock()
	if job != nil {
		<-job.done
	}
	synctest.Wait()
	initial, ok := service.SearchState(query, "owner")
	if !ok || initial.Status != LiveSearchReady || initial.Detail == nil || !initial.Detail.DigestsPending ||
		len(initial.Detail.Candidates) != 1 || collections.Load() != 1 || digests.Load() != 3 {
		t.Fatalf("fixture did not reach a partial pending answer: %+v, sources=%d, digests=%d",
			initial, collections.Load(), digests.Load())
	}
	if service.reusableSearch(*initial.Detail, query) != (mode == "shortfall") {
		t.Fatal("fixture did not preserve the model-failure cache guard")
	}
	time.Sleep(liveSearchFailWindow + time.Second)
	state, err = service.StartSearch(t.Context(), query, "owner", false)
	if err != nil || state.Status != LiveSearchReady || state.Detail == nil ||
		state.Detail.RunID != initial.Detail.RunID || !state.Detail.DigestsPending || collections.Load() != 1 {
		t.Fatalf("poll restarted pending search: status=%s, sources=%d, error=%v", state.Status, collections.Load(), err)
	}
	if mode == "refresh" {
		state, err = service.StartSearch(t.Context(), query, "owner", true)
		if err != nil || state.Status != LiveSearchRunning {
			t.Fatalf("explicit refresh refused: %+v %v", state, err)
		}
		service.mu.RLock()
		job = service.searchJobs[key]
		service.mu.RUnlock()
		if job != nil {
			<-job.done
		}
		synctest.Wait()
		time.Sleep(20 * time.Second)
		synctest.Wait()
		state, ok = service.SearchState(query, "owner")
		if !ok || state.Detail == nil || state.Detail.RunID == initial.Detail.RunID || !state.Detail.DigestsPending {
			t.Fatalf("old digest completion replaced a fresh run: %+v", state)
		}
	}
	time.Sleep(liveLateDigestWait)
	synctest.Wait()
	state, err = service.StartSearch(t.Context(), query, "owner", false)
	if err != nil || state.Status != LiveSearchReady || state.Detail == nil || state.Detail.DigestsPending {
		t.Fatalf("completed digests did not reach polling: %+v %v", state, err)
	}
	wantRuns := int64(1)
	if mode == "refresh" {
		wantRuns++
	}
	if collections.Load() != wantRuns || int64(digests.Load()) != 3*wantRuns ||
		mode != "shortfall" && service.searchJobs[key] != job {
		t.Fatalf("poll duplicated work: sources=%d, digests=%d", collections.Load(), digests.Load())
	}
	if initial.Detail.Passports["fixture:0"].DigestModel != "" || !initial.Detail.DigestsPending {
		t.Fatal("late delivery mutated a previously returned response")
	}
	if len(state.Detail.Candidates) != 1 || state.Detail.Candidates[0].Card == nil ||
		state.Detail.Candidates[0].Card.Summary != initial.Detail.Candidates[0].Card.Summary {
		t.Fatal("late delivery lost the partial candidate or rewrote its card")
	}
	missing := slices.ContainsFunc(state.Detail.Candidates[0].Limits, func(limit LiveLimit) bool {
		return limit.Code == liveLimitDigestFailed
	})
	filled := 0
	for _, passport := range state.Detail.Passports {
		if passport.DigestModel == "digest-model" {
			filled++
		}
	}
	wantFilled := 3
	switch mode {
	case "digest-timeout":
		wantFilled = 0
	case "partial-timeout":
		wantFilled = 1
	}
	if missing != (wantFilled < 3) || filled != wantFilled {
		t.Fatal("digest completion lost the successful text or hid the failed source")
	}
	if mode != "shortfall" {
		if _, cached := service.cache[key]; cached || len(store.stored) != 0 || service.reusableSearch(*state.Detail, query) {
			t.Fatal("diagnostic answer bypassed the durable cache guard")
		}
	} else if _, cached := service.cache[key]; !cached || len(store.stored) == 0 {
		t.Fatal("a reusable partial answer no longer follows normal persistence")
	}
}
