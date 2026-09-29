package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

const genArray = `[{"title":"Improve coating","statement":"Adding X raises hardness",` +
	`"rationale":"based on doc","trl":4,"novelty":0.6,"risk":0.4,"value":0.7,"confidence":0.6,` +
	`"measurable":true,"material_system":"Al2O3","process_change":"anneal","target_property":"hardness",` +
	`"causal_chain":[{"stage":"процесс","change":"anneal"},{"stage":"свойство","change":"hardness"}]}]`

func TestGenerate_PersistsHypotheses(t *testing.T) {
	gen := reply(genArray, &commonv1.Source{DocumentId: "d1", Filename: "p.pdf", Snippet: "X raises hardness in Al2O3", Score: 0.9})
	// Следующие проходы получают последний ответ; для этих необязательных шагов он безвреден.
	a := newAnswerer(gen, reply("[]"))
	cat := newCatalog()
	graph := newGraph()
	svc := NewHypothesisService(cat, a, nil, nil, nil, graph, nil)

	kpi := &commonv1.KPI{Id: "kpi1", OwnerId: "alice", Title: "Hardness", Metric: "HV"}
	created, err := svc.Generate(context.Background(), "alice", kpi, 1, "без кобальта, бюджет до 1 млн")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("want 1 hypothesis, got %d", len(created))
	}
	if created[0].GetOwnerId() != "alice" || created[0].CompositeScore == nil {
		t.Fatalf("created hypothesis must be owned and ranked: %+v", created[0])
	}
	if !strings.Contains(a.requests[0].GetPrompt(), "без кобальта") {
		t.Fatal("constraints must be woven into the generation prompt")
	}
	if !strings.Contains(created[0].GetGeneration(), "без кобальта") {
		t.Fatalf("constraints must be recorded in generation provenance: %s", created[0].GetGeneration())
	}
	if !hasString(created[0].GetTags(), "on_demand") {
		t.Fatalf("on-demand generation must be filterable by tag, got %v", created[0].GetTags())
	}
	if len(graph.edges["alice"]) == 0 {
		t.Fatal("Generate should enrich the knowledge graph")
	}
}

func TestGenerate_ExcludesAnswerDocs(t *testing.T) {
	gen := reply(genArray, &commonv1.Source{DocumentId: "d1", Filename: "p.pdf", Snippet: "X raises hardness", Score: 0.9})
	a := newAnswerer(gen, reply("[]"))
	cat := newCatalog()
	cat.docs = []*commonv1.Document{
		{Id: "ans1", OwnerId: "alice", Filename: "Гипотезы КГМК.docx", Kind: "hypotheses"},
		{Id: "d1", OwnerId: "alice", Filename: "p.pdf"},
	}
	chunks := &fakeChunks{chunks: map[string][]*llmv1.DocumentChunk{
		"ans1": {{Index: 0, Text: "Гипотезы по результатам мозгового штурма: 1. Магнитная сепарация."}},
	}}
	svc := NewHypothesisService(cat, a, nil, nil, nil, newGraph(), chunks)

	kpi := &commonv1.KPI{Id: "kpi1", OwnerId: "alice", Title: "Hardness", Metric: "HV"}
	if _, err := svc.Generate(context.Background(), "alice", kpi, 1, ""); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !hasString(a.requests[0].GetExcludeDocumentIds(), "ans1") {
		t.Fatalf("answer doc must be excluded from retrieval, got %v", a.requests[0].GetExcludeDocumentIds())
	}
	if !strings.Contains(a.requests[0].GetPrompt(), "Уже предложенные идеи") ||
		!strings.Contains(a.requests[0].GetPrompt(), "Магнитная сепарация") {
		t.Fatal("answer-doc content must appear as prior-art to avoid, not as evidence")
	}
}

func hasString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestGenerate_RetriesInstructionLikeTitle(t *testing.T) {
	bad := `[{"title":"Проверить подход из «coating.pdf» для KPI",` +
		`"statement":"Если нанести покрытие X, то коррозионная стойкость возрастёт.",` +
		`"rationale":"based on doc"}]`
	good := `[{"title":"Покрытие X повысит коррозионную стойкость",` +
		`"statement":"Если нанести покрытие X, то коррозионная стойкость возрастёт.",` +
		`"rationale":"based on doc"}]`
	a := newAnswerer(
		reply(bad, &commonv1.Source{DocumentId: "d1", Filename: "coating.pdf", Snippet: "coating improves corrosion resistance"}),
		reply(good, &commonv1.Source{DocumentId: "d1", Filename: "coating.pdf", Snippet: "coating improves corrosion resistance"}),
		reply("[]"),
	)
	svc := newHypService(newCatalog(), a)
	kpi := &commonv1.KPI{Id: "k", Title: "Повысить коррозионную стойкость"}
	created, err := svc.Generate(context.Background(), "alice", kpi, 1, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("want 1 hypothesis, got %d", len(created))
	}
	if got := created[0].GetTitle(); strings.HasPrefix(strings.ToLower(got), "проверить ") {
		t.Fatalf("instruction-like title must not be persisted, got %q", got)
	}
	if a.callCount() < 2 {
		t.Fatalf("invalid title must trigger a retry, got %d calls", a.callCount())
	}
}

func TestGenerate_RetriesNoisyDemoTitle(t *testing.T) {
	bad := `[{"title":"Если применить изменение режима термообработки, то деньги улучшится",` +
		`"statement":"Если применить изменение режима термообработки, то деньги улучшится.",` +
		`"rationale":"bad legacy demo row"}]`
	good := `[{"title":"Термообработка Ni-сплава повысит предел текучести при 800 °C",` +
		`"statement":"Если подобрать режим старения Ni-сплава, то предел текучести при 800 °C возрастёт.",` +
		`"rationale":"контекст связывает старение и прочность никелевых сплавов"}]`
	a := newAnswerer(
		reply(bad, &commonv1.Source{DocumentId: "d1", Filename: "nickel.pdf", Snippet: "aging improves nickel alloy strength"}),
		reply(good, &commonv1.Source{DocumentId: "d1", Filename: "nickel.pdf", Snippet: "aging improves nickel alloy strength"}),
		reply("[]"),
	)
	svc := newHypService(newCatalog(), a)
	kpi := &commonv1.KPI{Id: "k", Title: "Повысить жаропрочность Ni-сплава"}
	created, err := svc.Generate(context.Background(), "alice", kpi, 1, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("want 1 hypothesis, got %d", len(created))
	}
	if got := strings.ToLower(created[0].GetTitle()); strings.Contains(got, "деньги") {
		t.Fatalf("noisy title must not be persisted, got %q", created[0].GetTitle())
	}
	if a.callCount() < 2 {
		t.Fatalf("noisy title must trigger a retry, got %d calls", a.callCount())
	}
}

func TestGenerateTransportFailureDoesNotStartAnotherRetryCycle(t *testing.T) {
	a := newAnswerer(failReply(errors.New("llm down")))
	svc := newHypService(newCatalog(), a)
	if _, err := svc.Generate(context.Background(), "alice", &commonv1.KPI{Id: "k", Title: "T"}, 1, ""); err == nil {
		t.Fatal("a total LLM failure must surface")
	}
	if a.callCount() != 1 {
		t.Fatalf("transport retries were multiplied: %d calls", a.callCount())
	}
}

func TestGenerate_CountClamped(t *testing.T) {
	// Два пригодных элемента; запрос 0 даёт 5 по умолчанию, поэтому выживают оба.
	two := `[{"title":"A","statement":"sa","rationale":"ra"},{"title":"B","statement":"sb","rationale":"rb"}]`
	a := newAnswerer(reply(two), reply("[]"))
	svc := newHypService(newCatalog(), a)
	created, err := svc.Generate(context.Background(), "alice", &commonv1.KPI{Id: "k", Title: "T"}, 0, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("want 2 created, got %d", len(created))
	}
}

func TestGenerate_CreateError(t *testing.T) {
	cat := newCatalog()
	cat.createHypErr = errors.New("db down")
	a := newAnswerer(reply(genArray), reply("[]"))
	svc := newHypService(cat, a)
	if _, err := svc.Generate(context.Background(), "alice", &commonv1.KPI{Id: "k", Title: "T"}, 1, ""); err == nil {
		t.Fatal("create failure must surface")
	}
}

func TestFeedbackNote(t *testing.T) {
	cat := newCatalog()
	cat.list = []*commonv1.Hypothesis{{Id: "h1", OwnerId: "alice", Title: "Rejected idea", Status: "rejected"}}
	svc := newHypService(cat, nil)
	note := svc.feedbackNote(context.Background(), "alice", &commonv1.KPI{Id: "kpi1"})
	if note == "" {
		t.Fatal("feedback note should be non-empty when history exists")
	}
	if got := svc.feedbackNote(context.Background(), "alice", &commonv1.KPI{}); got != "" {
		t.Fatalf("blank KPI id must yield no feedback, got %q", got)
	}
}

func TestGenerate_KeepsBestResponseAndItsEvidence(t *testing.T) {
	first := `[{"title":"First","statement":"coating improves hardness","rationale":"first evidence"},` +
		`{"title":"Second","statement":"annealing improves hardness","rationale":"first evidence"}]`
	for _, tc := range []struct {
		name  string
		later string
	}{
		{"weaker", `[{"title":"Weaker","statement":"later statement","rationale":"later evidence"}]`},
		{"equal", `[{"title":"Later one","statement":"later statement","rationale":"later evidence"},` +
			`{"title":"Later two","statement":"another statement","rationale":"later evidence"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			best := reply(first, &commonv1.Source{DocumentId: "first-doc", Snippet: "first evidence improves hardness"})
			best.resp.Model = "first-model"
			later := reply(tc.later, &commonv1.Source{DocumentId: "later-doc", Snippet: "later evidence improves hardness"})
			later.resp.Model = "later-model"
			a := newAnswerer(best, later, failReply(errors.New("last attempt unavailable")))
			svc := newHypService(newCatalog(), a)
			scope := []string{"first-doc", "later-doc"}
			created, err := svc.GenerateFromDocuments(t.Context(), "alice", &commonv1.KPI{Title: "Hardness"}, 3, "", scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(created) != 2 {
				t.Fatalf("got %d hypotheses, want the two from the best response", len(created))
			}
			for i, wantTitle := range []string{"First", "Second"} {
				h := created[i]
				if h.GetTitle() != wantTitle {
					t.Fatalf("hypothesis %d title = %q, want %q", i, h.GetTitle(), wantTitle)
				}
				var generation struct {
					Model string `json:"model"`
					Kind  string `json:"kind"`
				}
				if err := json.Unmarshal([]byte(h.GetGeneration()), &generation); err != nil {
					t.Fatal(err)
				}
				if generation.Model != "first-model" || generation.Kind != "on_demand" {
					t.Fatalf("best response provenance was replaced: %+v", generation)
				}
				if evidence := h.GetEvidence(); len(evidence) != 1 || evidence[0].GetDocumentId() != "first-doc" {
					t.Fatalf("hypothesis %d must retain the best response's evidence: %v", i, evidence)
				}
			}
			if a.callCount() != genLLMAttempts {
				t.Fatalf("got %d calls, want all %d generation attempts", a.callCount(), genLLMAttempts)
			}
			for i, req := range a.requests {
				if !slices.Equal(req.GetScopeDocumentIds(), scope) {
					t.Fatalf("attempt %d scope = %v, want %v", i, req.GetScopeDocumentIds(), scope)
				}
			}
		})
	}
}

func TestGenerateFromDocuments_DisablesClusterFallback(t *testing.T) {
	cat := newCatalog()
	cat.clusters["outside"] = &commonv1.Cluster{
		Id: "outside", OwnerId: "alice", Label: "Coatings", DocumentCount: 1,
		Representatives: `[{"document_id":"outside-doc","filename":"coating.pdf",` +
			`"snippet":"Protective coating reduces corrosion rate."}]`,
	}
	outage := errors.New("llm unavailable")
	a := newAnswerer(failReply(outage))
	svc := newHypService(cat, a)
	scope := []string{"selected-doc"}
	created, err := svc.GenerateFromDocuments(t.Context(), "alice", &commonv1.KPI{Title: "Reduce corrosion"}, 3, "", scope)
	if !errors.Is(err, outage) {
		t.Fatalf("scoped generation must surface the outage instead of using outside evidence: %v", err)
	}
	if len(created) != 0 || len(cat.hypotheses) != 0 {
		t.Fatalf("scoped generation persisted cluster fallback hypotheses: %v", created)
	}
	if a.callCount() != 1 {
		t.Fatalf("transport retries were multiplied: %d calls", a.callCount())
	}
	for i, req := range a.requests {
		if !slices.Equal(req.GetScopeDocumentIds(), scope) {
			t.Fatalf("attempt %d scope = %v, want %v", i, req.GetScopeDocumentIds(), scope)
		}
	}
}
