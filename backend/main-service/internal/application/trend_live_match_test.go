package application

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLiveQueryMatchesArxivSyntaxLocally(t *testing.T) {
	tests := []struct {
		query, title, abstract string
		want                   bool
	}{
		{`all:"graph rag" OR all:graphrag`, "GraphRAG at scale", "", true},
		{`all:"graph rag" OR all:graphrag`, "Retrieval", "A graph-RAG pipeline", true},
		{`all:"graph rag"`, "Graph of RAG systems", "", false},
		{`all:"kv cache" AND (all:compression OR all:eviction)`, "KV-cache eviction for LLMs", "", true},
		{`all:"kv cache" AND (all:compression OR all:eviction)`, "KV cache for LLMs", "", false},
		{`all:"agent memory" OR (all:"long-term memory" AND all:agents)`, "Long-term memory for an agent", "", true},
		{`all:CRISPR OR all:"gene editing"`, "CRISPR-Cas9 screens", "", true},
		{`all:"solid state battery" OR all:"solid-state batteries"`, "A Solid-State Battery Electrolyte", "", true},
		{`ti:"state space"`, "Mamba", "state space models", false},
		{`ti:"state space"`, "State Space Duality", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.query+"/"+tt.title, func(t *testing.T) {
			query, err := liveCompileQuery(tt.query)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			title := liveStems(liveTokens(tt.title))
			text := liveStems(liveTokens(tt.title + " " + tt.abstract))
			if got := query(title, text); got != tt.want {
				t.Fatalf("match = %v, want %v", got, tt.want)
			}
		})
	}
	for _, bad := range []string{``, `all:`, `all:"open`, `(all:a`, `all:a OR`, `foo AND all:b`, `all:a) OR all:b`} {
		if _, err := liveCompileQuery(bad); err == nil {
			t.Fatalf("query %q compiled", bad)
		}
	}
	seen := map[string]bool{}
	for _, family := range liveFamilies() {
		if _, err := liveCompileQuery(family.query.Arxiv); err != nil {
			t.Fatalf("family %s: %v", family.id, err)
		}
		if seen[family.id] {
			t.Fatalf("family %s declared twice", family.id)
		}
		seen[family.id] = true
		// Без латинских фраз реестр и патентные ведомства молча не дадут половины свидетельств.
		topic := LiveTopic{Query: family.query}
		if len(liveScholarPhrases(liveTopicTerms(&topic))) == 0 {
			t.Fatalf("family %s has no Latin search terms", family.id)
		}
	}
}

func TestLiveGitHubReusesClosedMonths(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("q"))
		fmt.Fprint(w, `{"total_count":7,"items":[]}`)
	}))
	defer server.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	previous := &LiveSnapshot{Periods: livePeriods(now.AddDate(0, -1, 0))}
	repos := make([]int, len(previous.Periods))
	for i := range repos {
		repos[i] = i + 1
	}
	family := liveFamilies()[0]
	previous.Topics = []LiveTopic{
		{StudyTopic: StudyTopic{ID: family.id}, Query: family.query, Repos: repos},
		{StudyTopic: StudyTopic{ID: "other"}, Query: LiveQuery{GitHub: "x"}, Repos: []int{1}},
	}
	src := newLiveSources(func(context.Context) string { return "tok" })
	src.github, src.sleep = server.URL, func(context.Context, time.Duration) error { return nil }
	src.priorRepos = livePriorRepos(previous)
	if _, ok := src.priorRepos["other\nx"]; ok {
		t.Fatal("a history of the wrong length was reused")
	}
	periods := livePeriods(now)
	topic := LiveTopic{StudyTopic: StudyTopic{ID: family.id}, Query: family.query}
	liveGitHub(t.Context(), src, &topic, periods, true)
	// Прошлый снимок снят в августе: 12 закрытых месяцев переиспользованы,
	// август (тогда неполный) и сентябрь спрошены заново, плюс топ.
	if len(queries) != 3 || !strings.Contains(queries[0], "created:2026-08-01..") ||
		!strings.Contains(queries[1], "created:2026-09-01..") || !strings.Contains(queries[2], "created:>=") {
		t.Fatalf("github queries: %q", queries)
	}
	if len(topic.Repos) != 14 || topic.Repos[0] != 2 || topic.Repos[11] != 13 || topic.Repos[12] != 7 || topic.Repos[13] != 7 {
		t.Fatalf("repos: %v", topic.Repos)
	}
}
