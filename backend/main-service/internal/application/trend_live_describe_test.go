package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestDescribeTopicsSamplesWholeDirection(t *testing.T) {
	// Поздние работы лежат в снимке первыми, как общие с семейством, собранным раньше.
	papers, ids := make([]StudyPaper, 24), make([]string, 24)
	for i := range papers {
		ids[i] = fmt.Sprintf("p%02d", i)
		papers[i] = StudyPaper{ID: ids[i], Title: "Work " + ids[i], Date: fmt.Sprintf("2026-01-%02d", i+1)}
	}
	papers = append(papers[12:], papers[:12]...)
	snapshot := LiveSnapshot{
		Topics: []LiveTopic{{ID: "a", PaperIDs: ids, Name: "Alpha", Description: "старое"},
			{ID: "b", PaperIDs: []string{"p00"}, Name: "Beta", Description: "уже есть"}},
		Discovered:    []LiveTopic{{ID: "c", PaperIDs: []string{"p00"}, Name: "Gamma", Label: "Гамма"}},
		Papers:        papers,
		PaperMetadata: map[string]LivePaperMetadata{"p00": {Abstract: "Abstract text"}},
	}
	var mu sync.Mutex
	seen := map[string][]LiveDescribedWork{}
	s := &TrendLiveService{}
	s.SetDescriber(func(_ context.Context, name string, works []LiveDescribedWork) (string, error) {
		mu.Lock()
		seen[name] = works
		mu.Unlock()
		if name == "Beta" {
			return "", errors.New("шлюз недоступен")
		}
		return "Описание " + name, nil
	})
	s.describeTopics(t.Context(), &snapshot)
	alpha := seen["Alpha"]
	if len(alpha) != 12 || alpha[0].Title != "Work p00" || alpha[0].Abstract != "Abstract text" ||
		alpha[11].Title != "Work p23" {
		t.Fatalf("выборка не по всему направлению: %+v", alpha)
	}
	if snapshot.Topics[0].Description != "Описание Alpha" || snapshot.Topics[1].Description != "уже есть" ||
		snapshot.Discovered[0].Description != "Описание Гамма" {
		t.Fatalf("topics %+v %+v", snapshot.Topics, snapshot.Discovered)
	}
}

func TestDescribeSearchFillsUmbrellaTopic(t *testing.T) {
	topic := LiveTopic{ID: "search:рубль", Name: "цифровой рубль", PaperIDs: []string{"p1"}}
	papers := []StudyPaper{{ID: "p0", Title: "Чужая работа"}, {ID: "p1", Title: "Своя работа"}}
	metadata := map[string]LivePaperMetadata{"p1": {Abstract: "Аннотация"}}
	s := &TrendLiveService{}
	s.SetDescriber(func(_ context.Context, name string, works []LiveDescribedWork) (string, error) {
		if name != "цифровой рубль" || len(works) != 1 || works[0].Title != "Своя работа" {
			return "", errors.New("чужие работы в запросе")
		}
		return "Описание запроса", nil
	})
	join := s.describeSearch(t.Context(), &topic, papers, metadata)
	// Карточки пишутся в ту же структуру, пока описание идёт в фоне.
	topic.Label = "Цифровой рубль"
	join()
	if topic.Description != "Описание запроса" {
		t.Fatalf("описание %q", topic.Description)
	}
	described := LiveTopic{Description: "уже есть", PaperIDs: []string{"p1"}}
	s.describeSearch(t.Context(), &described, papers, metadata)()
	if described.Description != "уже есть" {
		t.Fatalf("описание перезаписано: %q", described.Description)
	}
}

func TestNewTopicDescriberRejectsEnglishReply(t *testing.T) {
	on := func(context.Context) bool { return true }
	works := []LiveDescribedWork{{Title: "T", Abstract: "A"}}
	if _, err := NewTopicDescriber(fakeGenerator{"Graph retrieval over knowledge graphs."}, on)(
		t.Context(), "GraphRAG", works); err == nil {
		t.Fatal("английское описание принято")
	}
	got, err := NewTopicDescriber(fakeGenerator{" «Поиск по графам знаний.» "}, on)(t.Context(), "GraphRAG", works)
	if err != nil || got != "Поиск по графам знаний." {
		t.Fatalf("описание %q %v", got, err)
	}
	if _, err := NewTopicDescriber(fakeGenerator{"Поиск по графам"}, func(context.Context) bool { return false })(
		t.Context(), "GraphRAG", works); !errors.Is(err, errLLMNotConfigured) {
		t.Fatalf("без шлюза: %v", err)
	}
}

func TestLiveUsableName(t *testing.T) {
	query := LiveQuery{Terms: []string{"graph rag", "knowledge graph"}}
	for name, want := range map[string]string{
		"undefined": "graph rag, knowledge graph", " NULL ": "graph rag, knowledge graph", "": "graph rag, knowledge graph",
		" GraphRAG ": "GraphRAG",
	} {
		if got := liveUsableName(LiveDiscovery{ID: "d1", Name: name, Query: query}); got != want {
			t.Errorf("name %q → %q, want %q", name, got, want)
		}
	}
	if got := liveUsableName(LiveDiscovery{ID: "d1", Name: "undefined"}); got != "d1" {
		t.Errorf("без термов: %q, want d1", got)
	}
}
