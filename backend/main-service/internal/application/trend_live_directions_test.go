package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLiveDirectionDetailCarriesTechnologies(t *testing.T) {
	topic := LiveTopic{ID: "agent-memory", Name: "Agent memory", PaperIDs: []string{"arxiv:1", "arxiv:2"},
		Limits: []LiveLimit{{Code: "snapshot"}}}
	snapshot := &LiveSnapshot{ObservedAt: "2026-09-22T12:45:16Z", Topics: []LiveTopic{topic},
		Papers:        []StudyPaper{{ID: "arxiv:1"}, {ID: "arxiv:2"}, {ID: "arxiv:3"}},
		PaperMetadata: map[string]LivePaperMetadata{"arxiv:1": {Language: "en"}}}
	answer := liveDirectionAnswer{Contract: liveSearchContract,
		Candidates: []LiveTopic{{ID: "memory-guard", PaperIDs: []string{"arxiv:1"}, EvidenceIDs: []string{"record:1"}}},
		Rejected:   []LiveRejection{{Name: "noise"}},
		Documents:  []StudyPaper{{ID: "record:1"}},
		Metadata:   map[string]LivePaperMetadata{"record:1": {Digest: "пересказ"}},
		Limits:     []LiveLimit{{Code: "qualification"}}}

	detail := liveDirectionDetail(snapshot, topic, answer)

	if len(detail.Candidates) != 1 || detail.Counters.Selected != 1 || detail.Counters.Rejected != 1 {
		t.Fatalf("technologies lost: %d candidates, counters %+v", len(detail.Candidates), detail.Counters)
	}
	if detail.SearchContract != liveSearchContract {
		t.Fatalf("contract %q: the page cannot tell a counted direction from a pending one", detail.SearchContract)
	}
	if detail.Passports["record:1"].Digest != "пересказ" {
		t.Fatalf("evidence record lost its digest: %+v", detail.Passports)
	}
	if len(detail.Topic.Limits) != 2 || len(snapshot.Topics[0].Limits) != 1 {
		t.Fatalf("limits: detail %v, snapshot %v", detail.Topic.Limits, snapshot.Topics[0].Limits)
	}
}

func TestDirectionScopeChangeDoesNotRepeatFailedRecount(t *testing.T) {
	for _, mode := range []string{"failed", "empty"} {
		t.Run(mode, func(t *testing.T) {
			const query = "Новое направление"
			service := NewTrendLiveService(nil, nil)
			service.card = func(context.Context, string, LiveCardIdentity, []string) (LiveCard, error) {
				t.Fatal("an empty local corpus must not generate a card")
				return LiveCard{}, nil
			}
			topic := LiveTopic{ID: "same-id", Name: query}
			service.snapshot = &LiveSnapshot{ObservedAt: "2026-09-27", Topics: []LiveTopic{topic}}
			previous := liveScopedDetailFixture("Прежняя область", LiveDetail{
				Candidates: []LiveTopic{storedCandidateFixture("old-candidate")}})
			service.directions[topic.ID] = liveDirectionAnswer{Scope: previous.Scope, Candidates: previous.Candidates}
			calls := 0
			service.SetPlanner(func(context.Context, string) (LiveQueryPlan, error) {
				calls++
				if mode == "failed" {
					return LiveQueryPlan{}, errors.New("planner unavailable")
				}
				return liveFixtureQueryPlan(query, []string{"new technology"}), nil
			})
			service.refreshDirections(t.Context())
			service.refreshDirections(t.Context())
			if calls != 1 || service.directionTried[topic.ID] != service.snapshot.ObservedAt {
				t.Fatalf("invalid old scope created a repeated background run: calls=%d tried=%q", calls,
					service.directionTried[topic.ID])
			}
			shown, found := service.Topic(topic.ID)
			if !found || len(shown.Candidates) != 0 || shown.Scope != nil {
				t.Fatalf("old differently scoped answer was exposed: %+v", shown)
			}
		})
	}
}

func TestLegacyDirectionAnswerShownUntilRecount(t *testing.T) {
	topic := LiveTopic{ID: "agent-memory", Name: "Agent memory"}
	store := memoryTestStore{liveDirectionsPrefix + topic.ID + ".json": []byte(
		`{"contract":"search-v11","rules":8,"candidates":[{"id":"memory-guard","name":"Memory guard"}]}`)}
	service := NewTrendLiveService(store, nil)
	service.snapshot = &LiveSnapshot{ObservedAt: "2026-09-27", Topics: []LiveTopic{topic}}

	service.loadDirections(t.Context(), service.snapshot.Topics)

	shown, found := service.Topic(topic.ID)
	if !found || len(shown.Candidates) != 1 || shown.SearchContract != "search-v11" {
		t.Fatalf("legacy technologies hidden until a recount that takes hours: %+v", shown.Candidates)
	}
	if service.freshDirection(service.directions[topic.ID], service.snapshot) {
		t.Fatal("legacy answer must still be recounted under the current rules")
	}
}

func TestDirectionsRecountTwoAtOnceWeakFirst(t *testing.T) {
	service := NewTrendLiveService(memoryTestStore{}, nil)
	service.card = func(context.Context, string, LiveCardIdentity, []string) (LiveCard, error) {
		return LiveCard{}, nil
	}
	service.snapshot = &LiveSnapshot{ObservedAt: "2026-09-27", Topics: []LiveTopic{
		{Name: "Зрелое", StudyTopic: StudyTopic{ID: "mature", Stage: "established"}},
		{Name: "Ранний", StudyTopic: StudyTopic{ID: "early", Stage: liveStageEarly}},
		{Name: "Зарождается", StudyTopic: StudyTopic{ID: "emerging", Stage: liveStageEmerging}},
	}}
	var (
		mu             sync.Mutex
		order          []string
		inflight, peak int
	)
	service.SetPlanner(func(_ context.Context, query string) (LiveQueryPlan, error) {
		mu.Lock()
		order = append(order, query)
		inflight++
		peak = max(peak, inflight)
		mu.Unlock()
		// Второй пересчёт должен начаться, пока идёт первый.
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			mu.Lock()
			both := peak >= liveDirectionParallel
			mu.Unlock()
			if both {
				break
			}
		}
		mu.Lock()
		inflight--
		mu.Unlock()
		return LiveQueryPlan{}, errors.New("stop after planning")
	})
	service.refreshDirections(t.Context())
	if peak != liveDirectionParallel || len(order) != 3 || order[2] != "Зрелое" {
		t.Fatalf("want two recounts at once, weak signals first: peak=%d order=%v", peak, order)
	}
}
