package application

import (
	"context"
	"encoding/json/v2"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/analysisdoc"
)

type blockedLiveSearchStore struct {
	*fakeSearchStore
	block   func(LiveDetail) bool
	started chan analysisdoc.LiveSearchSave
	release chan struct{}
	once    sync.Once
	resume  sync.Once
}

func newBlockedLiveSearchStore(t *testing.T, block func(LiveDetail) bool) *blockedLiveSearchStore {
	t.Helper()
	store := &blockedLiveSearchStore{fakeSearchStore: newFakeSearchStore(), block: block,
		started: make(chan analysisdoc.LiveSearchSave, 1), release: make(chan struct{})}
	t.Cleanup(store.unblock)
	return store
}

func (s *blockedLiveSearchStore) unblock() { s.resume.Do(func() { close(s.release) }) }

func (s *blockedLiveSearchStore) SaveLiveSearch(ctx context.Context, req analysisdoc.LiveSearchSave,
) (analysisdoc.LiveSearchStored, error) {
	var detail LiveDetail
	if err := json.Unmarshal(req.Detail, &detail); err != nil {
		return analysisdoc.LiveSearchStored{}, err
	}
	if s.block(detail) {
		s.once.Do(func() {
			s.started <- req
			<-s.release
		})
	}
	return s.fakeSearchStore.SaveLiveSearch(ctx, req)
}

// Блокировка стоит перед атомарной записью, поэтому второй экземпляр сервиса успевает сохранить свой результат.
func TestLiveSearchPersistenceRejectsLateWriters(t *testing.T) {
	for _, mode := range []string{"first-save", "repair-new-run", "repair-same-run", "repair-new-incomplete-run"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) { testLiveLateWriter(t, mode) })
		})
	}
}

func testLiveLateWriter(t *testing.T, mode string) {
	t.Helper()
	service, original := liveCachedDigestFixture(t)
	store := newBlockedLiveSearchStore(t, func(detail LiveDetail) bool {
		return detail.Passports["missing"].Digest == "Поздний пересказ."
	})
	service.SetSearchStore(store)
	if mode != "first-save" {
		var ok bool
		original, ok = service.saveSearch(t.Context(), "query", original, time.Now().Add(time.Hour))
		if !ok {
			t.Fatal("initial snapshot was not saved")
		}
	}
	key := liveSearchKey("owner", "query")
	service.cache[key] = liveCacheEntry{detail: original, expires: time.Now().Add(time.Hour), digestAttempts: 1}
	if mode == "repair-new-incomplete-run" {
		entry := service.cache[key]
		entry.digestAttempts = liveCachedDigestAttempts - 1
		service.cache[key] = entry
	}
	service.digest = func(context.Context, string, string) (string, string, error) {
		return "Поздний пересказ.", "model", nil
	}
	if mode == "first-save" {
		late := liveWithDigests(original, map[string]liveDigest{"missing": {text: "Поздний пересказ.", model: "model"}})
		go service.persistSearch(t.Context(), "query", key, late, time.Now().Add(time.Hour))
	} else if _, err := service.StartSearch(t.Context(), "query", "owner", false); err != nil {
		t.Fatal(err)
	}
	request := <-store.started
	if request.IfMatch == nil || *request.IfMatch != original.storeVersion {
		t.Fatalf("writer lost its initial precondition: %+v", request.IfMatch)
	}
	newer := liveWithDigests(original, map[string]liveDigest{"missing": {text: "Новый пересказ.", model: "model"}})
	if mode == "repair-new-incomplete-run" {
		passport := newer.Passports["missing"]
		passport.DigestModel = ""
		newer.Passports["missing"] = passport
	}
	if mode != "repair-same-run" {
		newer.RunID = "replacement-run"
	}
	other := storeService(t, store)
	winner, ok := other.saveSearch(t.Context(), "query", newer, time.Now().Add(time.Hour))
	if !ok || winner.storeVersion == original.storeVersion {
		t.Fatal("second replica did not commit a new version")
	}
	store.unblock()
	synctest.Wait()
	saved, found := service.storedSearch(t.Context(), "query", nil)
	state, known := service.SearchState("query", "owner")
	if !found || !known || state.Detail == nil {
		t.Fatal("winner disappeared from storage or cache")
	}
	for _, detail := range []LiveDetail{saved, *state.Detail} {
		if detail.RunID != winner.RunID || detail.storeVersion != winner.storeVersion || detail.DigestsPending ||
			detail.Passports["missing"].Digest != "Новый пересказ." {
			t.Fatalf("stale writer overwrote the winner: %+v", detail)
		}
	}
	if mode == "repair-new-incomplete-run" {
		if service.cache[key].digestAttempts != 1 {
			t.Fatal("the replacement run inherited exhausted repair attempts")
		}
		if _, err := service.StartSearch(t.Context(), "query", "owner", false); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		filled, ok := service.storedSearch(t.Context(), "query", nil)
		if !ok || filled.RunID != winner.RunID || filled.Passports["missing"].DigestModel != "model" {
			t.Fatal("the replacement run could not repair its missing digest")
		}
	}
}

func TestLiveFreshSearchRetainsItsBaselineRun(t *testing.T) {
	store := newFakeSearchStore()
	service := storeService(t, store)
	old, ok := service.saveSearch(t.Context(), "query", LiveDetail{Scope: liveFixtureQueryScope("query"),
		RunID: "old"}, time.Now().Add(time.Hour))
	if !ok {
		t.Fatal("initial snapshot was not saved")
	}
	version, runID, known := service.searchVersion(t.Context(), "query")
	fresh := LiveDetail{Scope: liveFixtureQueryScope("query"), RunID: "new", storeVersion: version, storeRunID: runID}
	if !known || runID != "old" || version == "" {
		t.Fatal("fresh search did not capture its baseline before collecting")
	}
	old.Passports = map[string]LivePaperPassport{"paper": {Digest: "Пересказ.", DigestModel: "model"}}
	if _, saved := service.saveSearch(t.Context(), "query", old, time.Now().Add(time.Hour)); !saved {
		t.Fatal("concurrent auxiliary update failed")
	}
	winner, saved := service.saveSearch(t.Context(), "query", fresh, time.Now().Add(time.Hour))
	if !saved || winner.RunID != "new" || winner.storeRunID != "new" || liveSearchBaseline(winner) != "" {
		t.Fatalf("old auxiliary metadata displaced the fresh search, or its next repair kept the old baseline: %+v", winner)
	}
}

func TestLiveRescoreDoesNotPublishAfterConflictReadFailure(t *testing.T) {
	store := newFakeSearchStore()
	service := storeService(t, store)
	service.classifierVersion = "old"
	candidate := storedCandidateFixture("c1")
	candidate.ModelScore = &LiveModelScore{Version: "old", Value: .8}
	previous, ok := service.saveSearch(t.Context(), "query", liveScopedDetailFixture("query",
		LiveDetail{RunID: "run", ClassifierVersion: "old",
			Candidates: []LiveTopic{candidate}}), time.Now().Add(time.Hour))
	if !ok {
		t.Fatal("initial snapshot was not saved")
	}
	service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		return LiveClassifierResponse{FeatureSchema: LiveClassifierSchema, Predictions: []LiveClassifierPrediction{{
			ID: "c1", LiveModelScore: LiveModelScore{Label: liveClassifierWeak, Value: .9, Threshold: .75,
				Model: "test", Version: input.ModelVersion, FeatureSchema: LiveClassifierSchema,
				Basis: "Проверочная модельная оценка", Features: []LiveModelFeature{{Name: "Рост", Value: "1", Contribution: .1}}},
		}}}, nil
	}, "new")
	previous.storeVersion, store.failOn = "stale-version", "read"
	returned, changed := service.rescoreStored(t.Context(), "query", previous)
	if changed || returned.ClassifierVersion != "old" || returned.Candidates[0].ModelScore != candidate.ModelScore {
		t.Fatalf("unacknowledged rescore replaced the known snapshot: %+v", returned)
	}
}

// Завершение первого пересказа ещё не разрешает ремонт: опрос ждёт подтверждения записи и её новую версию.
func TestLiveSearchInitialPersistenceCoalescesRepair(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, detail := liveCachedDigestFixture(t)
		store := newBlockedLiveSearchStore(t, func(saved LiveDetail) bool {
			return saved.Passports["missing"].DigestModel == ""
		})
		service.SetSearchStore(store)
		var calls atomic.Int32
		service.digest = func(context.Context, string, string) (string, string, error) {
			calls.Add(1)
			return "Исправленный пересказ.", "model", nil
		}
		late := make(chan map[string]liveDigest, 1)
		late <- nil
		detail.DigestsPending, detail.lateDigests = true, late
		key := liveSearchKey("owner", "query")
		service.cache[key] = liveCacheEntry{detail: detail, expires: time.Now().Add(time.Hour), digestAttempts: 1}
		go service.persistSearch(t.Context(), "query", key, detail, time.Now().Add(time.Hour))
		<-store.started
		for range 12 {
			state, err := service.StartSearch(t.Context(), "query", "owner", false)
			if err != nil || state.Detail == nil || !state.Detail.DigestsPending {
				t.Fatalf("pending cleared before commit: %+v %v", state, err)
			}
		}
		if calls.Load() != 0 {
			t.Fatal("polls started auxiliary writes before the initial snapshot committed")
		}
		store.unblock()
		synctest.Wait()
		if _, err := service.StartSearch(t.Context(), "query", "owner", false); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		saved, found := service.storedSearch(t.Context(), "query", nil)
		if !found || saved.DigestsPending || calls.Load() != 1 || saved.Passports["missing"].DigestModel != "model" {
			t.Fatalf("repair did not inherit the committed version: %+v, calls=%d", saved, calls.Load())
		}
	})
}
