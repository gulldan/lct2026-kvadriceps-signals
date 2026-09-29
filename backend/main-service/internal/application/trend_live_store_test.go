package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/indexdoc"
)

type fakeSearchStore struct {
	mu     sync.Mutex
	stored map[string]analysisdoc.LiveSearchSave
	byKey  map[string]string
	fresh  bool
	failOn string
}

func newFakeSearchStore() *fakeSearchStore {
	return &fakeSearchStore{stored: map[string]analysisdoc.LiveSearchSave{}, byKey: map[string]string{}, fresh: true}
}

func (f *fakeSearchStore) ReadLiveSearch(
	_ context.Context, req analysisdoc.LiveSearchRead,
) (analysisdoc.LiveSearchStored, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn == "read" {
		return analysisdoc.LiveSearchStored{}, errors.New("база недоступна")
	}
	query := req.Query
	if _, ok := f.stored[query]; !ok {
		query = f.byKey[req.TermsKey]
	}
	saved, ok := f.stored[query]
	if !ok {
		return analysisdoc.LiveSearchStored{}, nil
	}
	version := ""
	if req.WithVersion {
		version = indexdoc.Hash(saved.Detail)
	}
	return analysisdoc.LiveSearchStored{
		Version: version,
		Found:   true, Fresh: f.fresh, Query: saved.Query, TermsKey: saved.TermsKey,
		Terms: saved.Terms, Papers: saved.Papers, Sources: len(saved.Records), Detail: saved.Detail,
	}, nil
}

func (f *fakeSearchStore) SaveLiveSearch(
	_ context.Context, req analysisdoc.LiveSearchSave,
) (analysisdoc.LiveSearchStored, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn == "save" {
		return analysisdoc.LiveSearchStored{}, errors.New("база недоступна")
	}
	if req.IfMatch != nil {
		current, found := f.stored[req.Query]
		version := ""
		if found {
			version = indexdoc.Hash(current.Detail)
		}
		var before, after LiveDetail
		fresh := req.BaselineRunID != "" && *req.IfMatch != "" &&
			json.Unmarshal(current.Detail, &before) == nil && json.Unmarshal(req.Detail, &after) == nil &&
			before.RunID == req.BaselineRunID && after.RunID != "" && after.RunID != before.RunID
		if version != *req.IfMatch && !fresh {
			return analysisdoc.LiveSearchStored{}, analysisdoc.ErrConflict
		}
	}
	f.stored[req.Query] = req
	f.byKey[req.TermsKey] = req.Query
	return analysisdoc.LiveSearchStored{Found: true, Fresh: true, Version: indexdoc.Hash(req.Detail)}, nil
}

func TestLiveTermsKeyIgnoresOrderAndCase(t *testing.T) {
	a := liveTermsKey([]string{"Solid Oxide", "  electrolysis  "})
	b := liveTermsKey([]string{"electrolysis", "solid oxide", "electrolysis"})
	if a != b || a == "" {
		t.Fatalf("ключи разошлись: %q против %q", a, b)
	}
}

func storeService(t *testing.T, store LiveSearchStore) *TrendLiveService {
	t.Helper()
	s := &TrendLiveService{cache: map[string]liveCacheEntry{}, now: time.Now}
	s.SetSearchStore(store)
	return s
}

func storedCandidateFixture(name string) LiveTopic {
	const quote = "A phase comparison detects synthetic speech."
	label, source := "Проверяемая технология "+name, "source:"+name
	return LiveTopic{ID: name, Name: name, Label: label, PaperIDs: []string{source},
		PapersCount: 1, DocumentsCount: 1,
		Qualification: &LiveQualification{Scope: liveFixtureQueryScope("запрос"), Label: label,
			Mechanism:  "Сравнение фаз сигнала выявляет синтетическую речь",
			Technology: true, Relevant: true, Coherent: true, Verified: true, Method: liveQualificationMethod,
			Model: "reader", VerifierModel: "reviewer", Reason: "Источник описывает проверенный механизм",
			Evidence: []LiveQualificationEvidence{{ScopeCriteria: []string{"b1"}, SourceID: source, Quote: quote,
				Role: "introduces", SameTechnology: true,
				IdentityReason: "Описание источника соответствует механизму", MechanismQuote: quote, RelevanceQuote: quote,
				RelevanceReason: "Источник описывает запрошенную проверку речи"}}}}
}

func TestStoredSearchReturnsSavedDetail(t *testing.T) {
	store := newFakeSearchStore()
	detail := LiveDetail{Topic: LiveTopic{ID: "search:электролиз", PapersCount: 12,
		Query: LiveQuery{Terms: []string{"solid oxide electrolysis"}}}}
	s := storeService(t, store)
	s.saveSearch(t.Context(), "электролиз", liveScopedDetailFixture("электролиз", detail), time.Now().Add(time.Hour))

	restored, ok := s.storedSearch(t.Context(), "электролиз", nil)
	if !ok {
		t.Fatal("сохранённый поиск не найден")
	}
	if restored.Topic.PapersCount != 12 {
		t.Errorf("статей %d, ожидали 12", restored.Topic.PapersCount)
	}
}

// Недобор прежнего отбора пересобирается, даже если сохранённые карточки сами по себе пригодны.
func TestStoredSearchDoesNotReuseEarlierSelectionBudget(t *testing.T) {
	store := newFakeSearchStore()
	s := storeService(t, store)
	detail := LiveDetail{SearchContract: "search-v17", Candidates: []LiveTopic{storedCandidateFixture("первая")},
		Limits: []LiveLimit{{Code: liveRejectBudget}}}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveLiveSearch(t.Context(), analysisdoc.LiveSearchSave{
		Query: "search-v17|запрос", Detail: raw, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found := s.storedSearch(t.Context(), "запрос", nil); found {
		t.Fatal("earlier selection budget was reused instead of the current queue policy")
	}
}

// Несобранный запрос даёт пустой ответ, а не сбор; наружу уходят первые технологии без вкладов признаков.
func TestSearchTopReadsStoredOnly(t *testing.T) {
	s := storeService(t, newFakeSearchStore())
	candidates := []LiveTopic{storedCandidateFixture("первая"), storedCandidateFixture("вторая"),
		storedCandidateFixture("третья")}
	candidates[0].ModelScore = &LiveModelScore{Value: 0.9, Features: []LiveModelFeature{{Name: "x"}}}
	s.saveSearch(t.Context(), "антифрод",
		liveScopedDetailFixture("антифрод", LiveDetail{ObservedAt: "2026-09-24", Candidates: candidates}), time.Now().Add(time.Hour))

	top, err := s.SearchTop(t.Context(), "  Антифрод ", 2)
	if err != nil || !top.Found || len(top.Candidates) != 2 || top.Candidates[0].ModelScore.Value != 0.9 ||
		top.Candidates[0].ModelScore.Features != nil {
		t.Fatalf("выдача тезиса: %+v, %v", top, err)
	}
	missing, err := s.SearchTop(t.Context(), "не искали", 5)
	if err != nil || missing.Found || missing.Candidates == nil || len(s.searchJobs) != 0 {
		t.Fatalf("несобранный запрос: %+v, %v", missing, err)
	}
}

// Одинаковые поисковые термины могут скрывать противоположную задачу.
func TestStoredSearchDoesNotReuseAnotherIntentByTerms(t *testing.T) {
	store := newFakeSearchStore()
	s := storeService(t, store)
	detail := LiveDetail{Topic: LiveTopic{Query: LiveQuery{Terms: []string{"voice", "deepfake"}}}}
	s.saveSearch(t.Context(), "создание синтетической речи",
		liveScopedDetailFixture("создание синтетической речи", detail), time.Now().Add(time.Hour))

	if _, ok := s.storedSearch(t.Context(), "обнаружение синтетической речи", []string{"deepfake", "voice"}); ok {
		t.Fatal("оценка другой задачи переиспользована по совпадению терминов")
	}
}

// Устаревшая запись открывается из истории сразу, а обновляет её ночная пересборка.
func TestStoredSearchReturnsStale(t *testing.T) {
	store := newFakeSearchStore()
	s := storeService(t, store)
	s.saveSearch(t.Context(), "запрос",
		LiveDetail{Scope: liveFixtureQueryScope("запрос"), Topic: LiveTopic{ID: "search:запрос"}}, time.Now().Add(-time.Hour))
	store.fresh = false
	if _, ok := s.storedSearch(t.Context(), "запрос", nil); !ok {
		t.Fatal("устаревшая запись не открылась")
	}
	if len(s.searchTopics) != 1 || s.searchTopics[0].ID != "search:запрос" {
		t.Fatalf("сохранённый запрос не попал в обзор: %+v", s.searchTopics)
	}
}

// Закреплённые пересобираются первыми и сверх потолка, остальные — свежие первыми и по разу на текст.
func TestLiveSearchesToRefresh(t *testing.T) {
	snapshotAt := time.Date(2026, 9, 22, 2, 0, 0, 0, time.UTC)
	pinned := livePinnedSearches()
	entries := make([]analysisdoc.LiveSearchEntry, 0, 8+liveRefreshSearches)
	entries = append(entries,
		analysisdoc.LiveSearchEntry{Query: liveStoreKey(pinned[0]), ObservedAt: snapshotAt.Add(time.Hour)},
		analysisdoc.LiveSearchEntry{Query: liveStoreKey("свежий"), ObservedAt: snapshotAt.Add(time.Hour)},
		analysisdoc.LiveSearchEntry{Query: liveStoreKey("старый"), ObservedAt: snapshotAt.Add(-time.Hour)},
		analysisdoc.LiveSearchEntry{Query: "search-v10|старый", ObservedAt: snapshotAt.Add(-2 * time.Hour)},
		analysisdoc.LiveSearchEntry{Query: "search-v10|свежий", ObservedAt: snapshotAt.Add(-3 * time.Hour)},
		analysisdoc.LiveSearchEntry{Query: "без версии", ObservedAt: snapshotAt.Add(-time.Hour)},
	)
	for i := range liveRefreshSearches + 3 {
		entries = append(entries, analysisdoc.LiveSearchEntry{
			Query: liveStoreKey("запрос " + string(rune('а'+i))), ObservedAt: snapshotAt.Add(-time.Hour)})
	}
	opened := map[string]bool{}
	for _, entry := range entries {
		_, query, _ := strings.Cut(entry.Query, "|")
		opened[query] = true
	}
	queries := liveSearchesToRefresh(entries, snapshotAt, snapshotAt.Add(liveSearchRefreshAge), opened)
	if len(queries) != len(pinned)-1+liveRefreshSearches || !slices.Equal(queries[:len(pinned)-1], pinned[1:]) ||
		queries[len(pinned)-1] != "старый" || slices.Contains(queries, "свежий") {
		t.Fatalf("отбор пересборки: %v", queries)
	}
	if young := liveSearchesToRefresh(entries, snapshotAt, snapshotAt.Add(time.Hour), opened); slices.Contains(young, "старый") {
		t.Fatalf("пересборка ответа моложе суток: %v", young)
	}
	// Неоткрытый за сутки запрос ночью не пересобирается: OpenAlex на него не тратится.
	queries = liveSearchesToRefresh(entries, snapshotAt, snapshotAt.Add(liveSearchRefreshAge), nil)
	if len(queries) != len(pinned)-1 {
		t.Fatalf("пересобран неоткрытый запрос: %v", queries)
	}
}

// Недоступная база не должна ронять поиск: ответ уже получен пользователем.
func TestSearchStoreFailuresAreSurvivable(t *testing.T) {
	store := newFakeSearchStore()
	store.failOn = "read"
	s := storeService(t, store)
	if _, ok := s.storedSearch(t.Context(), "запрос", nil); ok {
		t.Fatal("отказ чтения выдал результат")
	}
	store.failOn = "save"
	s.saveSearch(t.Context(), "запрос", LiveDetail{Scope: liveFixtureQueryScope("запрос")}, time.Now().Add(time.Hour))

	none := storeService(t, nil)
	if _, ok := none.storedSearch(t.Context(), "запрос", nil); ok {
		t.Fatal("без хранилища найден результат")
	}
	none.saveSearch(t.Context(), "запрос", LiveDetail{Scope: liveFixtureQueryScope("запрос")}, time.Now())
}

// Без типа, языка и уровня доверенности источника выдача не выполняет требование к источникам.
func TestLiveStoredRecordsKeepsPassport(t *testing.T) {
	detail := LiveDetail{Topic: LiveTopic{
		Patents: &LiveChannel{Records: []LiveSourceRecord{
			{URL: "https://uspto.gov/a", Source: "USPTO", Title: "Патент", Date: "2026-08-01",
				Language: "en", PublisherType: PublisherPatentOffice, Trust: TrustHigh, TrustBasis: "ведомство"},
		}},
		Web: &LiveChannel{Records: []LiveSourceRecord{
			{URL: "https://nplus1.ru/b", Source: "N+1", Language: "ru", Trust: TrustMedium, TrustBasis: "медиа"},
			{URL: "", Source: "без адреса"},
			{URL: "https://uspto.gov/a", Source: "дубль"},
		}},
		External: []StudyEvent{{URL: "https://github.com/o/r", Kind: "implementation",
			Trust: TrustLowered, TrustBasis: "самопубликация"}},
	}}
	records := liveStoredRecords(detail)
	if len(records) != 3 {
		t.Fatalf("записей %d, ожидали 3 (пустой адрес и дубль отбрасываются): %+v", len(records), records)
	}
	if records[0].Language != "en" || records[0].Trust != TrustHigh || records[0].PublisherType == "" {
		t.Errorf("паспорт патента потерян: %+v", records[0])
	}
	if records[1].Language != "ru" {
		t.Errorf("язык русского источника потерян: %+v", records[1])
	}
	if records[2].Trust != TrustLowered {
		t.Errorf("репозиторий обязан идти с пониженной доверенностью: %+v", records[2])
	}
}

func TestStoredDetailRoundTrips(t *testing.T) {
	store := newFakeSearchStore()
	s := storeService(t, store)
	detail := LiveDetail{Topic: LiveTopic{ID: "search:x", Name: "x",
		Web: &LiveChannel{Records: []LiveSourceRecord{{URL: "https://nplus1.ru/b", Trust: TrustMedium}}}}}
	s.saveSearch(t.Context(), "x", liveScopedDetailFixture("x", detail), time.Now().Add(time.Hour))
	// Ключ версионирован: выкатка с новым форматом выдачи обязана обесценить сохранённое.
	saved := store.stored[liveStoreKey("x")]
	var decoded LiveDetail
	if err := json.Unmarshal(saved.Detail, &decoded); err != nil {
		t.Fatalf("разбор сохранённого ответа: %v", err)
	}
	if decoded.Topic.Web == nil || len(decoded.Topic.Web.Records) != 1 {
		t.Fatalf("каналы не пережили запись: %+v", decoded.Topic)
	}
	if len(saved.Records) != 1 {
		t.Fatalf("сырые записи не сохранены: %+v", saved.Records)
	}
}

func (f *fakeSearchStore) ListLiveSearches(_ context.Context, req analysisdoc.LiveSearchList,
) (analysisdoc.LiveSearchHistory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	history := analysisdoc.LiveSearchHistory{Entries: []analysisdoc.LiveSearchEntry{}}
	for key, saved := range f.stored {
		if req.Limit > 0 && len(history.Entries) == req.Limit {
			break
		}
		history.Entries = append(history.Entries, analysisdoc.LiveSearchEntry{
			Query: key, Terms: saved.Terms, Papers: saved.Papers, Summary: saved.Summary,
			Sources: len(saved.Records), ObservedAt: saved.ObservedAt, ExpiresAt: saved.ExpiresAt,
		})
	}
	return history, nil
}

func TestSearchHistoryHidesStorageVersion(t *testing.T) {
	store := newFakeSearchStore()
	s := storeService(t, store)
	s.saveSearch(t.Context(), "финтех",
		LiveDetail{Scope: liveFixtureQueryScope("финтех"), Topic: LiveTopic{ID: "x"}}, time.Now().Add(time.Hour))
	history, err := s.SearchHistory(t.Context(), 10)
	if err != nil {
		t.Fatalf("история: %v", err)
	}
	if len(history.Entries) != 1 || history.Entries[0].Query != "финтех" {
		t.Fatalf("запрос показан не тем, чем он был: %+v", history.Entries)
	}
}

func TestStoredQueryScopeCannotBypassQualification(t *testing.T) {
	const query = "проверка речи"
	for _, mode := range []string{"valid", "missing_scope", "different_query", "qualification_scope_changed",
		"missing_criterion", "unknown_criterion", "unverified", "unmet_shared_condition"} {
		t.Run(mode, func(t *testing.T) {
			detail := liveScopedDetailFixture(query, LiveDetail{SearchContract: liveSearchContract,
				Candidates: []LiveTopic{storedCandidateFixture("детектор")}})
			switch mode {
			case "missing_scope":
				detail.Scope = nil
			case "different_query":
				detail = liveScopedDetailFixture("создание синтетической речи", detail)
			case "qualification_scope_changed":
				detail.Candidates[0].Qualification.Scope.Branches[0].Task = "Создание синтетической речи"
			case "missing_criterion":
				detail.Candidates[0].Qualification.Evidence[0].ScopeCriteria = nil
			case "unknown_criterion":
				detail.Candidates[0].Qualification.Evidence[0].ScopeCriteria = []string{"b1", "b2"}
			case "unverified":
				detail.Candidates[0].Qualification.Verified = false
			case "unmet_shared_condition":
				detail.Scope.Required = []LiveScopeCondition{{ID: "c1", Condition: "Телефонные платежи"}}
				detail.Candidates[0].Qualification.Scope = liveCloneQueryScope(detail.Scope)
			}
			store := newFakeSearchStore()
			raw, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			store.stored[liveStoreKey(query)] = analysisdoc.LiveSearchSave{Query: liveStoreKey(query), Detail: raw}
			service := storeService(t, store)
			service.cache[liveSearchKey("owner", query)] = liveCacheEntry{detail: detail, expires: time.Now().Add(time.Hour)}
			_, persisted := service.storedSearch(t.Context(), query, nil)
			_, cached := service.SearchState(query, "owner")
			want := mode == "valid"
			if persisted != want || cached != want || service.reusableSearch(detail, query) != want {
				t.Fatalf("invalid scope crossed a storage boundary: stored=%v memory=%v want=%v", persisted, cached, want)
			}
			topic := LiveTopic{ID: "direction", Name: query}
			service.snapshot = &LiveSnapshot{ObservedAt: "2026-09-27", Topics: []LiveTopic{topic}}
			answer := liveDirectionAnswer{Scope: detail.Scope, Candidates: detail.Candidates,
				Snapshot: service.snapshot.ObservedAt, Contract: liveSearchContract, Rules: liveDirectionRules}
			service.directions = map[string]liveDirectionAnswer{topic.ID: answer}
			returned, found := service.Topic(topic.ID)
			if !found || (len(returned.Candidates) == 1) != want {
				t.Fatalf("direction exposed a differently scoped qualification: %+v", returned)
			}
		})
	}
}
