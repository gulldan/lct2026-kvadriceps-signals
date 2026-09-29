package application

import (
	"context"
	"strings"
	"testing"

	"github.com/example/llm-service/internal/domain"
)

// Различает вызовы по промпту: с «Passages:» — сжатие, иначе — выбор следующего шага.
type scriptedReasoner struct {
	actions       []string
	idx           int
	condenseReply string
	actionCalls   int
	condenseCalls int
}

func (r *scriptedReasoner) Reason(_ context.Context, _, user string) (string, error) {
	if strings.Contains(user, "Passages:") {
		r.condenseCalls++
		return r.condenseReply, nil
	}
	r.actionCalls++
	if r.idx < len(r.actions) {
		out := r.actions[r.idx]
		r.idx++
		return out, nil
	}
	return "[answer]", nil
}

type fakeGraph struct {
	related  []string
	gotSeeds []string
}

func (f *fakeGraph) Expand(_ context.Context, _ string, seeds []string, _ int) ([]string, error) {
	f.gotSeeds = seeds
	return f.related, nil
}

func TestAgenticGate(t *testing.T) {
	cases := []struct {
		name, text, prompt string
		want               bool
	}{
		{"simple factual", "What is the boiling point of water?", "", false},
		{"single entity only", "Tell me about the Roman Empire please.", "", false},
		{"comparative english", "Compare the efficiency of design A and design B.", "", true},
		{"comparative russian", "В чём разница между методом А и методом Б?", "", true},
		{
			"multi-clause multi-entity",
			"How did the Roman Empire and the Han Dynasty manage trade, taxation and military over time?",
			"", true,
		},
		{"structured prompt suppressed", "Compare design A versus design B across many factors here", "return JSON", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reasons := agenticGate(domain.Query{Text: c.text, Prompt: c.prompt})
			if got != c.want {
				t.Fatalf("agenticGate(%q) = %v (reasons %v), want %v", c.text, got, reasons, c.want)
			}
		})
	}
}

func TestParseAction(t *testing.T) {
	cases := []struct {
		in     string
		search bool
		query  string
	}{
		{"reasoning here\n[search] turbine blade material\nmore", true, "turbine blade material"},
		{"[SEARCH]   \"design B reliability\"  ", true, "design B reliability"},
		{"I can answer now.\n[answer]", false, ""},
		{"no action token at all", false, ""},
		{"[search]    ", false, ""},
	}
	for _, c := range cases {
		got := parseAction(c.in)
		if got.search != c.search || got.query != c.query {
			t.Errorf("parseAction(%q) = %+v, want search=%v query=%q", c.in, got, c.search, c.query)
		}
	}
}

func TestEntityCount(t *testing.T) {
	if n := entityCount("what is turbine efficiency"); n != 0 {
		t.Fatalf("lowercase query entity count = %d, want 0", n)
	}
	if n := entityCount("How do the Roman Empire and the Han Dynasty compare"); n < 2 {
		t.Fatalf("two named entities should count >= 2, got %d", n)
	}
}

func TestAgenticLoopGenerates(t *testing.T) {
	ans := &fakeAnswerer{}
	svc := build(fakeRetriever{chunks: makeChunks(4)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)
	rsn := &scriptedReasoner{actions: []string{"[search] design B reliability"}, condenseReply: "Design A is more efficient."}
	svc.EnableAgentic(AgenticConfig{MaxHops: 3}, rsn, nil)

	res, err := svc.Answer(context.Background(), domain.Query{
		OwnerID: "u1", Text: "Compare design A and design B efficiency and reliability",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !ans.called {
		t.Fatal("expected grounded generation on the agentic path")
	}
	if !ans.gotCite {
		t.Fatal("agentic final generation should request citations")
	}
	if rsn.actionCalls == 0 || rsn.condenseCalls == 0 {
		t.Fatalf("agentic loop not exercised: actions=%d condense=%d", rsn.actionCalls, rsn.condenseCalls)
	}
	if len(res.Sources) == 0 {
		t.Fatal("expected cited sources from the accumulated evidence")
	}
}

func TestAgenticFallThroughSimpleQuery(t *testing.T) {
	ans := &fakeAnswerer{}
	svc := build(fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)
	rsn := &scriptedReasoner{}
	svc.EnableAgentic(AgenticConfig{MaxHops: 3}, rsn, nil)

	res, err := svc.Answer(context.Background(), domain.Query{OwnerID: "u1", Text: "What is turbine efficiency?"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if rsn.actionCalls != 0 || rsn.condenseCalls != 0 {
		t.Fatalf("simple query must not enter the agentic loop: actions=%d condense=%d", rsn.actionCalls, rsn.condenseCalls)
	}
	if !ans.called || len(res.Sources) != 3 {
		t.Fatalf("single-shot path should answer: called=%v sources=%d", ans.called, len(res.Sources))
	}
}

func TestAgenticDisabledRunsSingleShot(t *testing.T) {
	ans := &fakeAnswerer{}
	svc := build(fakeRetriever{chunks: makeChunks(3)}, fakeRetriever{}, fakeRanker{score: 1}, ans, false)

	res, err := svc.Answer(context.Background(), domain.Query{
		OwnerID: "u1", Text: "Compare design A versus design B efficiency and reliability and cost",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !ans.called || len(res.Sources) != 3 {
		t.Fatalf("flag off: complex query must still take the single-shot path: called=%v sources=%d",
			ans.called, len(res.Sources))
	}
}

func TestAgenticGraphFold(t *testing.T) {
	ans := &fakeAnswerer{}
	cs := &fakeChunkSource{chunks: []domain.StoredChunk{{ID: "g0", Index: 0, Text: "graph neighbour fact"}}}
	svc := New(fakeEmbedder{}, fakeRetriever{chunks: makeChunks(2)}, fakeRetriever{}, fakeRanker{score: 1},
		ans, fakeCache{}, cs, nil, nil, 5, false, -5, false, Tuning{})
	fg := &fakeGraph{related: []string{"docX"}}
	rsn := &scriptedReasoner{condenseReply: "fact"}
	svc.EnableAgentic(AgenticConfig{MaxHops: 1, GraphTopN: 3}, rsn, fg)

	res, err := svc.Answer(context.Background(), domain.Query{
		OwnerID: "u1", Text: "Compare A and B and C across several distinct factors right here",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(fg.gotSeeds) == 0 {
		t.Fatal("graph expander was not seeded with evidence document ids")
	}
	found := false
	for _, src := range res.Sources {
		if src.ChunkID == "g0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("graph-expanded chunk not folded into the evidence sources: %+v", res.Sources)
	}
}
