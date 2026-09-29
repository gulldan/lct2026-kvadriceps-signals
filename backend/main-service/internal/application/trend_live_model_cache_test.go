package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/analysisdoc"
)

// Восстановление модели той же версии не должно требовать суток или перезапуска.
func TestSearchRetriesClassifierAfterDegradedResult(t *testing.T) {
	const query, version = "phase coherence", "same-artifact"
	store := &liveRetryStore{fakeSearchStore: newFakeSearchStore(), saved: make(chan struct{}, 1)}
	service, collections := liveRetryService(t)
	service.SetSearchStore(store)
	var clock atomic.Int64
	clock.Store(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC).Unix())
	service.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	var healthy atomic.Bool
	var calls atomic.Int64
	service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		calls.Add(1)
		if !healthy.Load() {
			return LiveClassifierResponse{}, errors.New("модель временно недоступна")
		}
		output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
		for _, candidate := range input.Candidates {
			output.Predictions = append(output.Predictions, LiveClassifierPrediction{
				ID: candidate.ID, LiveModelScore: LiveModelScore{
					Value: 0.82, Threshold: 0.75, Label: liveClassifierWeak, Model: "test-classifier",
					Version: version, FeatureSchema: LiveClassifierSchema, Basis: "Проверка восстановления транспорта модели.",
					Features: []LiveModelFeature{{Name: "Авторские группы", Value: "3", Contribution: 0.1}},
				},
			})
		}
		return output, nil
	}, version)
	degraded := LiveDetail{ClassifierVersion: version,
		Topic: LiveTopic{Limits: []LiveLimit{{Code: liveLimitModelFailed}}}}
	raw, err := json.Marshal(degraded)
	if err != nil {
		t.Fatal(err)
	}
	store.stored[liveStoreKey(query)] = analysisdoc.LiveSearchSave{Query: liveStoreKey(query), Detail: raw}
	service.cache[liveSearchKey("owner", query)] = liveCacheEntry{detail: degraded, expires: service.now().Add(liveSearchTTL)}
	if _, ok := service.storedSearch(t.Context(), query, nil); ok {
		t.Fatal("исторический отказ модели той же версии принят из базы")
	}
	if _, ok := service.SearchState(query, "owner"); ok {
		t.Fatal("исторический отказ модели принят из памяти")
	}
	run := func() *liveSearchJob {
		t.Helper()
		state, startErr := service.StartSearch(t.Context(), query, "owner", false)
		if startErr != nil || state.Status != LiveSearchRunning {
			t.Fatalf("повторный поиск не запущен: %+v, %v", state, startErr)
		}
		service.mu.RLock()
		job := service.searchJobs[liveSearchKey("owner", query)]
		service.mu.RUnlock()
		select {
		case <-job.done:
		case <-time.After(5 * time.Second):
			t.Fatal("локальный поиск не завершился")
		}
		return job
	}
	first := run()
	for range 10 {
		state, pollErr := service.StartSearch(t.Context(), query, "owner", false)
		if pollErr != nil || state.Status != LiveSearchReady || state.Detail == nil ||
			service.reusableSearch(*state.Detail, query) || calls.Load() != 1 || collections.Load() != 1 {
			t.Fatalf("опрос потерял итог или повторил сбор: %+v, %v; вызовов %d", state, pollErr, calls.Load())
		}
	}
	if _, ok := service.cache[liveSearchKey("owner", query)]; ok || service.searchJobs[liveSearchKey("owner", query)] != first {
		t.Fatal("отказ модели попал в суточный кэш или заменил завершённую попытку")
	}
	select {
	case <-store.saved:
		t.Fatal("отказ модели записан как пригодный суточный ответ")
	default:
	}
	healthy.Store(true)
	clock.Add(int64(liveReopenHold/time.Second) + 1)
	run()
	select {
	case <-store.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("восстановленный ответ не сохранён")
	}
	state, err := service.StartSearch(t.Context(), query, "owner", false)
	if err != nil || state.Status != LiveSearchReady || state.Detail == nil ||
		len(state.Detail.Candidates) != 1 || state.Detail.Candidates[0].ModelScore == nil ||
		state.Detail.Candidates[0].ModelScore.Version != version || calls.Load() != 2 || collections.Load() != 2 {
		t.Fatalf("модель той же версии не восстановилась: %+v, %v; вызовов %d", state, err, calls.Load())
	}
	if _, ok := service.storedSearch(t.Context(), query, nil); !ok {
		t.Fatal("восстановленный ответ не пригоден для повторного чтения")
	}
	withoutModel := storeService(t, newFakeSearchStore())
	withoutModel.saveSearch(t.Context(), query, LiveDetail{Scope: liveFixtureQueryScope(query), Topic: LiveTopic{
		Limits: []LiveLimit{{Code: liveLimitModelMissing}},
	}}, service.now().Add(liveSearchTTL))
	if _, ok := withoutModel.storedSearch(t.Context(), query, nil); !ok {
		t.Fatal("штатный режим без подключённой модели перестал кэшироваться")
	}
}

func TestRecoveredModelAnswerCachesOnlyCompleteCurrentScores(t *testing.T) {
	service, candidates, documents, periods := liveWaveFixture(t, liveTopCandidates)
	umbrella := LiveTopic{}
	candidates, _, _ = service.finishCandidates(t.Context(), "квантовое измерение", &umbrella,
		candidates, documents, nil, periods, nil)
	const version = "current-model"
	service.classifierVersion = version
	for i := range candidates {
		candidates[i].ModelScore = &LiveModelScore{Model: "fixture", Version: version, FeatureSchema: LiveClassifierSchema,
			Label: liveClassifierWeak, Value: .9, Threshold: .75}
	}
	recovered := LiveDetail{ClassifierVersion: version, Candidates: candidates, Papers: documents,
		Topic: LiveTopic{Limits: []LiveLimit{{Code: liveLimitModelFailed, Text: "отказ одной предыдущей волны"}}}}
	recovered = liveScopedDetailFixture("восстановленный поиск", recovered)
	for _, mode := range []string{"recovered_topic_limit", "recovered_detail_limit", "missing_score", "stale_score",
		"partial", "incomplete_card", "new_active_model"} {
		t.Run(mode, func(t *testing.T) {
			detail := recovered
			detail.Candidates = slices.Clone(recovered.Candidates)
			active := version
			usable, reusable := false, false
			switch mode {
			case "recovered_topic_limit":
				usable, reusable = true, true
			case "recovered_detail_limit":
				detail.Limits, detail.Topic.Limits = detail.Topic.Limits, nil
				usable, reusable = true, true
			case "missing_score":
				detail.Candidates[0].ModelScore = nil
			case "stale_score":
				score := *detail.Candidates[0].ModelScore
				score.Version = "old-model"
				detail.Candidates[0].ModelScore = &score
			case "partial":
				detail.Candidates = detail.Candidates[:liveTopCandidates-1]
			case "incomplete_card":
				card := *detail.Candidates[0].Card
				card.Summary = ""
				detail.Candidates[0].Card = &card
			case "new_active_model":
				active, usable = "next-model", true
			}
			service.classifierVersion = active
			if liveUsableStored(detail) != usable || service.reusableSearch(detail, "восстановленный поиск") != reusable {
				t.Fatalf("wrong recovered cache decision: usable=%v reusable=%v",
					liveUsableStored(detail), service.reusableSearch(detail, "восстановленный поиск"))
			}
		})
	}
	service.classifierVersion = version
	store := newFakeSearchStore()
	service.SetSearchStore(store)
	service.saveSearch(t.Context(), "восстановленный поиск", recovered, time.Now().Add(time.Hour))
	if restored, ok := service.storedSearch(t.Context(), "восстановленный поиск", nil); !ok ||
		!service.reusableSearch(restored, "восстановленный поиск") || len(restored.Topic.Limits) != 1 {
		t.Fatalf("recovered answer was lost or its failure history was erased: %+v, %v", restored.Topic.Limits, ok)
	}
	service.SetClassifier(func(context.Context, LiveClassifierRequest) (LiveClassifierResponse, error) {
		return LiveClassifierResponse{}, errors.New("new model temporarily unavailable")
	}, "next-model")
	failed, rescored := service.rescoreStored(t.Context(), "восстановленный поиск", recovered)
	if rescored || failed.ClassifierVersion != version || len(failed.Candidates) != liveTopCandidates ||
		len(failed.Topic.Limits) != 1 || failed.Candidates[0].ModelScore == nil ||
		failed.Candidates[0].ModelScore.Version != version || service.reusableSearch(failed, "восстановленный поиск") {
		t.Fatalf("failed rescore was accepted as recovery or destroyed old scores: %+v", failed)
	}
}

type liveRetryStore struct {
	*fakeSearchStore
	saved chan struct{}
}

func (s *liveRetryStore) SaveLiveSearch(ctx context.Context, req analysisdoc.LiveSearchSave,
) (analysisdoc.LiveSearchStored, error) {
	stored, err := s.fakeSearchStore.SaveLiveSearch(ctx, req)
	s.saved <- struct{}{}
	return stored, err
}

type liveRetryTransport func(*http.Request) (*http.Response, error)

func (f liveRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func liveRetryService(t *testing.T) (*TrendLiveService, *atomic.Int64) {
	t.Helper()
	return liveRetryServiceLanguage(t, "ru")
}

func liveRetryServiceLanguage(t *testing.T, language string) (*TrendLiveService, *atomic.Int64) {
	t.Helper()
	result := LiveWindowResult{Scope: liveFixtureQueryScope("phase coherence"),
		Metadata: map[string]LivePaperMetadata{}, Terms: []string{"phase coherence"}}
	for i := range 3 {
		id := fmt.Sprintf("fixture:%d", i)
		result.Papers = append(result.Papers, StudyPaper{ID: id, Title: "Phase coherence for synthetic speech",
			Date: fmt.Sprintf("2026-%02d-10", 5+i), Authors: []string{id}})
		result.Metadata[id] = LivePaperMetadata{Language: language, LanguageBasis: "локальная тестовая запись"}
	}
	result.Total = len(result.Papers)
	collections := new(atomic.Int64)
	service := NewTrendLiveService(nil, nil)
	service.collectorURL = "http://collector.test"
	service.snapshot = &LiveSnapshot{Periods: livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))}
	service.newSources = func() *liveSources {
		return &liveSources{github: "http://github.test", hn: "http://hn.test", ghDown: true,
			sleep: func(context.Context, time.Duration) error { return nil },
			http: &http.Client{Transport: liveRetryTransport(func(req *http.Request) (*http.Response, error) {
				body := []byte("{}")
				if req.URL.Host == "collector.test" {
					collections.Add(1)
					var request LiveWindowSearch
					if err := json.UnmarshalRead(req.Body, &request); err != nil {
						return nil, err
					}
					reply := result
					reply.Scope = request.Plan.Scope
					var err error
					body, err = json.Marshal(reply)
					if err != nil {
						return nil, err
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)),
					Header: http.Header{}}, nil
			})},
		}
	}
	service.SetPlanner(liveTestQueryPlanner(func(context.Context, string) ([]string, error) {
		return []string{"phase coherence"}, nil
	}))
	service.SetQualifier(func(_ context.Context, query, _ string, docs []LiveQualificationDocument,
	) (LiveQualification, error) {
		qualified := LiveQualification{Scope: liveFixtureQueryScope(query), Label: "Фазовое обнаружение синтезированной речи",
			Mechanism:  "Анализ фазовой согласованности",
			Technology: true, Relevant: true, Coherent: true, Verified: true, Reason: "Локальная проверка источников",
			Method: liveQualificationMethod, Model: "test", VerifierModel: "test"}
		for _, doc := range docs {
			qualified.Evidence = append(qualified.Evidence, LiveQualificationEvidence{ScopeCriteria: []string{"b1"}, SourceID: doc.ID,
				Quote: doc.Text, Role: "introduces", SameTechnology: true, IdentityReason: "Один тестовый механизм",
				MechanismQuote: doc.Text, RelevanceQuote: doc.Text, RelevanceReason: "Прямо описана требуемая задача"})
		}
		return qualified, nil
	})
	service.SetCardWriter(func(_ context.Context, _ string, _ LiveCardIdentity, works []string) (LiveCard, error) {
		card := LiveCard{Summary: "Описание", Advantage: "Преимущество", Example: "Эксперимент", VerificationModel: "test"}
		for _, field := range []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample} {
			card.Evidence = append(card.Evidence,
				LiveClaimEvidence{Field: field, SourceID: "1", Quote: works[0], Verified: true})
		}
		return card, nil
	})
	return service, collections
}
