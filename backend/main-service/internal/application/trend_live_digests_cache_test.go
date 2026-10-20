package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Опрос готового ответа восстанавливает пропущенный источник, не запуская новый отбор и не стирая готовые тексты.
func TestLiveCachedDigestRepair(t *testing.T) {
	for _, mode := range []string{"transient", "permanent", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, original := liveCachedDigestFixture(t)
				if mode != "timeout" {
					service.SetSearchStore(newFakeSearchStore())
				}
				started, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				service.digest = func(ctx context.Context, title, abstract string) (string, string, error) {
					call := calls.Add(1)
					if title != "Missing source" || abstract != "Original source abstract" {
						t.Errorf("repair changed the input or repeated a completed source: %q / %q", title, abstract)
					}
					started <- struct{}{}
					<-release
					switch {
					case mode == "timeout":
						<-ctx.Done()
						return "", "", fmt.Errorf("digest expired: %w", ctx.Err())
					case mode == "permanent" || call == 1:
						return "", "", errors.New("digest unavailable")
					default:
						return "Восстановленный пересказ.", "digest-model", nil
					}
				}
				for range 2 {
					state, err := service.StartSearch(t.Context(), "query", "owner", false)
					if err != nil || state.Status != LiveSearchReady || !state.Detail.DigestsPending {
						t.Fatalf("cached result did not begin auxiliary repair: %+v %v", state, err)
					}
					<-started
					var polls sync.WaitGroup
					for range 12 {
						polls.Go(func() {
							polled, err := service.StartSearch(t.Context(), "query", "owner", false)
							if err != nil || polled.Detail == nil || !polled.Detail.DigestsPending {
								t.Errorf("concurrent poll lost repair state: %+v %v", polled, err)
							}
						})
					}
					polls.Wait()
					release <- struct{}{}
					if mode == "timeout" {
						time.Sleep(liveLateDigestWait)
					}
					synctest.Wait()
				}
				for range 5 {
					state, err := service.StartSearch(t.Context(), "query", "owner", false)
					if err != nil || state.Detail == nil || state.Detail.DigestsPending || calls.Load() != 2 {
						t.Fatalf("repair did not stop after three total passes: calls=%d state=%+v err=%v", calls.Load(), state, err)
					}
					filled := state.Detail
					failed := slices.ContainsFunc(filled.Candidates[0].Limits, func(limit LiveLimit) bool {
						return limit.Code == liveLimitDigestFailed
					})
					if failed != (mode != "transient") || (len(liveUndigested(*filled)) == 0) != (mode == "transient") {
						t.Fatalf("final digest availability is hidden or wrong: %+v", filled)
					}
					if filled.RunID != original.RunID || len(filled.Candidates) != 1 ||
						!slices.Equal(filled.Candidates[0].PaperIDs, original.Candidates[0].PaperIDs) ||
						filled.Candidates[0].Card != original.Candidates[0].Card ||
						filled.Passports["done"] != original.Passports["done"] ||
						filled.Candidates[0].Limits[0].Code != "source_caveat" {
						t.Fatalf("auxiliary repair changed the selected answer or completed sources: %+v", filled)
					}
				}
				if original.Passports["missing"].DigestModel != "" || len(original.Candidates[0].Limits) != 1 {
					t.Fatal("cached auxiliary work mutated the original answer")
				}
				if mode == "timeout" {
					return
				}
				stored, ok := service.storedSearch(t.Context(), "query", nil)
				if !ok || stored.DigestsPending || (stored.Passports["missing"].DigestModel != "") != (mode == "transient") {
					t.Fatalf("completed auxiliary repair did not persist: %+v", stored)
				}
			})
		})
	}
}

func TestLiveCachedDigestRepairIgnoresReplacedRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, original := liveCachedDigestFixture(t)
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		service.digest = func(ctx context.Context, _, _ string) (string, string, error) {
			calls.Add(1)
			close(started)
			<-release
			if ctx.Err() != nil {
				t.Error("closing the requesting page canceled shared auxiliary work")
			}
			return "Поздний пересказ прежнего запуска.", "digest-model", nil
		}
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		state, err := service.StartSearch(canceled, "query", "owner", false)
		if err != nil || state.Detail.DigestsPending || calls.Load() != 0 {
			t.Fatalf("canceled request started repair: %+v %v", state, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		_, err = service.StartSearch(ctx, "query", "owner", false)
		if err != nil {
			t.Fatal(err)
		}
		<-started
		cancel()
		newer := original
		newer.RunID = "replacement-run"
		service.mu.Lock()
		service.cache[liveSearchKey("owner", "query")] = liveCacheEntry{detail: newer, expires: time.Now().Add(time.Hour)}
		service.mu.Unlock()
		close(release)
		synctest.Wait()
		state, known := service.SearchState("query", "owner")
		if !known || state.Detail.RunID != newer.RunID || state.Detail.DigestsPending ||
			state.Detail.Passports["missing"].DigestModel != "" {
			t.Fatalf("late repair overwrote a newer run: %+v", state)
		}
	})
}

func TestLiveDigestSourcesStopsQueuedWorkOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewTrendLiveService(nil, nil)
		var calls atomic.Int32
		service.digest = func(ctx context.Context, _, _ string) (string, string, error) {
			calls.Add(1)
			<-ctx.Done()
			return "", "", fmt.Errorf("digest canceled: %w", ctx.Err())
		}
		workers := int32(service.lateDigestWorkers(t.Context()))
		ids := make([]string, workers+5)
		for i := range ids {
			ids[i] = fmt.Sprintf("source-%d", i)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan map[string]liveDigest)
		go func() { done <- service.digestSources(ctx, ids, nil, nil) }()
		synctest.Wait()
		if calls.Load() != workers {
			t.Fatalf("workers were not bounded: %d", calls.Load())
		}
		cancel()
		if filled := <-done; len(filled) != 0 || calls.Load() != workers {
			t.Fatalf("queued sources started after cancellation: digests=%v calls=%d", filled, calls.Load())
		}
	})
}

func liveCachedDigestFixture(t *testing.T) (*TrendLiveService, LiveDetail) {
	t.Helper()
	service := NewTrendLiveService(nil, nil)
	service.newSources = func() *liveSources {
		t.Error("auxiliary repair must not collect sources")
		return nil
	}
	service.windowMeta = map[string]LivePaperMetadata{"missing": {Abstract: "Original source abstract"}}
	candidate := LiveTopic{PaperIDs: []string{"missing", "done", "ru"}, Card: &LiveCard{Summary: "Готовая карточка"},
		TZ: &LiveTZAssessment{StageCode: 1}, Limits: []LiveLimit{liveLimitOf("source_caveat", "Оговорка источника", nil)}}
	candidate.Qualification = &LiveQualification{Technology: true, Relevant: true, Coherent: true, Verified: true,
		Label: "Спектральный анализ", Mechanism: "Проверка спектра сигнала", Method: liveQualificationMethod,
		Evidence: []LiveQualificationEvidence{{SourceID: "missing", Quote: "Original source abstract", Role: "introduces",
			SameTechnology: true, IdentityReason: "Описан спектральный анализ", MechanismQuote: "Original source abstract",
			RelevanceQuote: "Original source abstract", RelevanceReason: "Проверяется нужный сигнал"}}}
	detail := LiveDetail{RunID: "original-run", Candidates: []LiveTopic{candidate},
		Papers: []StudyPaper{{ID: "missing", Title: "Missing source"}, {ID: "done", Title: "Completed source"},
			{ID: "ru", Title: "Русский источник"}},
		Passports: map[string]LivePaperPassport{
			"missing": {Language: "en", Digest: "Прежний текст без сведений о модели."},
			"done":    {Language: "en", Digest: "Готовый пересказ.", DigestModel: "original-model"},
			"ru":      {Language: "ru"},
		}}
	detail = liveScopedDetailFixture("query", detail)
	// Первая фоновая попытка уже завершилась без одного пересказа, как в живом ответе.
	service.cache[liveSearchKey("owner", "query")] = liveCacheEntry{
		detail: liveWithDigests(detail, nil), expires: time.Now().Add(time.Hour), digestAttempts: 1,
	}
	return service, detail
}
