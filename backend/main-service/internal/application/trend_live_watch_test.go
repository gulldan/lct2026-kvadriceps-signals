package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"testing"
	"time"
)

type memoryKV map[string][]byte

func (m memoryKV) SetJSON(_ context.Context, key string, v any, _ time.Duration) error {
	raw, err := json.Marshal(v)
	m[key] = raw
	return err
}

func (m memoryKV) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	raw, ok := m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, dest)
}

func TestLiveWatchesDigestWhatAppeared(t *testing.T) {
	ctx := t.Context()
	live := NewTrendLiveService(nil, func(context.Context) string { return "" })
	now := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	watches := NewLiveWatches(live, memoryKV{}, func(context.Context) ([]string, error) { return []string{"u"}, nil })
	watches.now = func() time.Time { return now }
	publish := func(stage string, papers int, technologies ...string) {
		observed := now.Format(time.RFC3339)
		topic := LiveTopic{StudyTopic: StudyTopic{ID: "mcp", Stage: stage}, Name: "MCP"}
		snapshot := &LiveSnapshot{ObservedAt: observed}
		for i := range papers {
			id := fmt.Sprint("paper-", i)
			topic.PaperIDs = append(topic.PaperIDs, id)
			snapshot.Papers = append(snapshot.Papers, StudyPaper{ID: id, URL: "https://example.org/" + id})
		}
		snapshot.Topics = []LiveTopic{topic}
		answer := liveDirectionAnswer{Snapshot: observed}
		for _, name := range technologies {
			answer.Candidates = append(answer.Candidates,
				LiveTopic{StudyTopic: StudyTopic{ID: "ngram:" + name, Stage: "early_signal"}, Name: name,
					Qualification: storedCandidateFixture(name).Qualification})
		}
		scoped := liveScopedDetailFixture(topic.Name, LiveDetail{Candidates: answer.Candidates})
		answer.Scope, answer.Candidates = scoped.Scope, scoped.Candidates
		live.mu.Lock()
		live.snapshot, live.directions["mcp"], live.directionTried["mcp"] = snapshot, answer, observed
		live.mu.Unlock()
	}
	digest := func() *LiveWatchDigest {
		t.Helper()
		watches.digest(ctx)
		list, err := watches.List(ctx, "u")
		if err != nil || len(list) != 1 {
			t.Fatalf("подписки: %v, %v", list, err)
		}
		return list[0].Digest
	}

	publish("early_signal", 2, "Агентные протоколы")
	if _, err := watches.Watch(ctx, "u", LiveWatchTopic, "mcp", "MCP"); err != nil {
		t.Fatal(err)
	}
	if got := digest(); got != nil {
		t.Fatalf("без нового снимка сводки нет, получено %+v", got)
	}

	now = now.Add(24 * time.Hour)
	publish("emerging_signal", 5, "Агентные протоколы", "Реестр инструментов")
	got := digest()
	if got == nil || !slices.Equal(got.Technologies, []string{"Реестр инструментов"}) || got.Sources != 3 ||
		got.Stage == nil || got.Stage.From != "early_signal" || got.Stage.To != "emerging_signal" {
		t.Fatalf("сводка после снимка: %+v", got)
	}

	now = now.Add(time.Hour)
	publish("emerging_signal", 5, "Агентные протоколы", "Реестр инструментов", "Песочница агентов")
	if got := digest(); len(got.Technologies) != 1 {
		t.Fatalf("чаще раза в сутки сводка не сверяется: %+v", got)
	}
	now = now.Add(liveWatchEvery)
	publish("emerging_signal", 5, "Агентные протоколы", "Реестр инструментов", "Песочница агентов")
	if got := digest(); len(got.Technologies) != 2 {
		t.Fatalf("непрочитанная сводка дорастает: %+v", got)
	}

	if err := watches.Ack(ctx, "u", LiveWatchTopic, "mcp"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(liveWatchEvery)
	publish("emerging_signal", 5, "Агентные протоколы", "Реестр инструментов", "Песочница агентов")
	if got := digest(); got != nil {
		t.Fatalf("после прочтения без изменений сводки нет: %+v", got)
	}
}
