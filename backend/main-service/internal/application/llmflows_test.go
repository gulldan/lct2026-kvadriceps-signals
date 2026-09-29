package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func seedHypothesis(a *scriptedAnswerer, h *commonv1.Hypothesis) (*fakeCatalog, *HypothesisService) {
	cat := newCatalog()
	cat.putHypothesis(h)
	return cat, newHypService(cat, a)
}

func TestVerify_PersistsVerdictAndEvidence(t *testing.T) {
	main := reply(`{"verdict":"supported","confidence":0.8,"rationale":"ok",`+
		`"claims":[{"claim":"alpha beta gamma delta","verdict":"supported","quote":"alpha beta gamma delta"}],`+
		`"supporting":["alpha beta gamma delta"],"contradicting":[]}`,
		&commonv1.Source{ChunkId: "c1", Filename: "p.pdf", Snippet: "alpha beta gamma delta epsilon"})
	counter := reply(`{"verdict":"insufficient","confidence":0.4,"rationale":"none",`+
		`"supporting":[],"contradicting":["zeta eta theta iota"]}`,
		&commonv1.Source{ChunkId: "c2", Filename: "q.pdf", Snippet: "zeta eta theta iota kappa"})
	a := newAnswerer(main, counter)
	cat, svc := seedHypothesis(a, &commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "alpha beta gamma delta"})

	got, err := svc.Verify(context.Background(), "alice", "h1")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ConfidenceScore == nil {
		t.Fatal("Verify must set a confidence (belief) score")
	}
	if !strings.Contains(got.GetAssessment(), "\"check\"") {
		t.Fatal("assessment.check must be written")
	}
	if cat.updateCalls == 0 {
		t.Fatal("Verify must persist via UpdateHypothesis")
	}
	if a.callCount() < 2 {
		t.Fatalf("Verify must run main + counter passes, got %d", a.callCount())
	}
}

func TestVerify_Forbidden(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("{}")), &commonv1.Hypothesis{Id: "h1", OwnerId: "alice"})
	if _, err := svc.Verify(context.Background(), "bob", "h1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign verify must be forbidden, got %v", err)
	}
}

func TestVerify_MainPassError(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(failReply(errors.New("llm down"))),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	if _, err := svc.Verify(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("main-pass error must surface")
	}
}

func TestVerify_ParseError(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("not json at all")),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	if _, err := svc.Verify(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("unparseable verdict must surface")
	}
}

func TestAssessTRL_GatesAndPersists(t *testing.T) {
	answer := `{"levels":[{"level":1,"met":true,"note":""},{"level":2,"met":true,"note":""},` +
		`{"level":3,"met":true,"note":""},{"level":4,"met":false,"note":"no prototype"},` +
		`{"level":5,"met":false},{"level":6,"met":false},{"level":7,"met":false},` +
		`{"level":8,"met":false},{"level":9,"met":false}]}`
	cat, svc := seedHypothesis(newAnswerer(reply(answer)),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	got, err := svc.AssessTRL(context.Background(), "alice", "h1")
	if err != nil {
		t.Fatalf("AssessTRL: %v", err)
	}
	if got.GetTrl() != 3 {
		t.Fatalf("gated TRL = %d, want 3", got.GetTrl())
	}
	if !strings.Contains(cat.hypotheses["h1"].GetAssessment(), "ugt-gating") {
		t.Fatal("assessment.trl must record the gating method")
	}
}

func TestAssessTRL_FloorsAtOne(t *testing.T) {
	answer := `{"levels":[{"level":1,"met":false}]}`
	_, svc := seedHypothesis(newAnswerer(reply(answer)),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	got, err := svc.AssessTRL(context.Background(), "alice", "h1")
	if err != nil {
		t.Fatalf("AssessTRL: %v", err)
	}
	if got.GetTrl() != 1 {
		t.Fatalf("a formulated hypothesis is at least УГТ 1, got %d", got.GetTrl())
	}
}

func TestAssessTRL_ParseError(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("garbage")),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	if _, err := svc.AssessTRL(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("unparseable anketa must surface")
	}
}

func TestTRLRubric(t *testing.T) {
	svc := newHypService(newCatalog(), nil)
	if len(svc.TRLRubric()) == 0 {
		t.Fatal("TRLRubric must be non-empty")
	}
}

func TestTagHypothesis_StoresClassification(t *testing.T) {
	// Заведомо фальшивый код validate() отбросит, а research_type настоящий.
	answer := `{"research_type":"практическое","grnti":[{"code":"99.99.99","name":"fake"}],` +
		`"vak":[],"asjc":[]}`
	cat, svc := seedHypothesis(newAnswerer(reply(answer)),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Title: "t", Statement: "s", Measurable: true})
	got, err := svc.TagHypothesis(context.Background(), "alice", "h1")
	if err != nil {
		t.Fatalf("TagHypothesis: %v", err)
	}
	if !strings.Contains(cat.hypotheses["h1"].GetDetail(), "classification") {
		t.Fatal("detail.classification must be written")
	}
	found := false
	for _, tag := range got.GetTags() {
		if tag == researchTagPractical {
			found = true
		}
	}
	if !found {
		t.Fatalf("research tag must be set, got %v", got.GetTags())
	}
}

func TestTagHypothesis_ParseError(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("nope")),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Title: "t"})
	if _, err := svc.TagHypothesis(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("parse error must surface")
	}
}

func TestITCRubric(t *testing.T) {
	svc := newHypService(newCatalog(), nil)
	if len(svc.ITCRubric()) == 0 {
		t.Fatal("ITCRubric must be non-empty")
	}
}

func TestAnalyzeCompetitors_StoresDetail(t *testing.T) {
	answer := `{"summary":"crowded","competitors":[{"name":"Acme","approach":"x",` +
		`"strengths":["a"],"weaknesses":["b"],"maturity":"TRL5","source":"doc.pdf"}]}`
	cat, svc := seedHypothesis(newAnswerer(reply(answer)),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Title: "t", Statement: "s"})
	if _, err := svc.AnalyzeCompetitors(context.Background(), "alice", "h1"); err != nil {
		t.Fatalf("AnalyzeCompetitors: %v", err)
	}
	if !strings.Contains(cat.hypotheses["h1"].GetDetail(), "competitors") {
		t.Fatal("detail.competitors must be written")
	}
}

func TestAnalyzeCompetitors_ParseError(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("nope")),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Title: "t"})
	if _, err := svc.AnalyzeCompetitors(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("parse error must surface")
	}
}

func TestPlanExperiment_StoresPlan(t *testing.T) {
	answer := `{"materials":["powder"],"process_parameters":[{"name":"temp","range":"600-800C"}],` +
		`"characterization_methods":["XRD"],"test_methods":["tensile"],"controls":["baseline"],` +
		`"success_criteria":["+10%"],"estimated_cost":"medium","estimated_time":"weeks","risks":["cracking"]}`
	cat, svc := seedHypothesis(newAnswerer(reply(answer)),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	if _, err := svc.PlanExperiment(context.Background(), "alice", "h1"); err != nil {
		t.Fatalf("PlanExperiment: %v", err)
	}
	if !strings.Contains(cat.hypotheses["h1"].GetDetail(), "experiment_plan") {
		t.Fatal("detail.experiment_plan must be written")
	}
}

func TestPlanExperiment_Errors(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("nope")),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	if _, err := svc.PlanExperiment(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("parse error must surface")
	}
	_, svc2 := seedHypothesis(newAnswerer(failReply(errors.New("llm down"))),
		&commonv1.Hypothesis{Id: "h2", OwnerId: "alice", Statement: "s"})
	if _, err := svc2.PlanExperiment(context.Background(), "alice", "h2"); err == nil {
		t.Fatal("LLM error must surface")
	}
}

// Составной балл из запроса игнорируется: composite_score считает только ранжировщик rank-v1.
func TestStoreITC_StoresAndLinks(t *testing.T) {
	cat, svc := seedHypothesis(newAnswerer(reply("{}")), &commonv1.Hypothesis{Id: "h1", OwnerId: "alice"})
	composite := 0.73
	in := ITCInput{ClusterID: "cl1", ITC: []byte(`{"score":7}`), Composite: &composite}
	got, err := svc.StoreITC(context.Background(), "alice", "h1", in)
	if err != nil {
		t.Fatalf("StoreITC: %v", err)
	}
	if got.GetPrimaryClusterId() != "cl1" {
		t.Fatalf("theme link = %q, want cl1", got.GetPrimaryClusterId())
	}
	if got.CompositeScore != nil {
		t.Fatalf("composite = %.2f, want untouched (rank-v1 owns it)", got.GetCompositeScore())
	}
	if !strings.Contains(cat.hypotheses["h1"].GetAssessment(), "itc") {
		t.Fatal("assessment.itc must be written")
	}
}

func TestStoreITC_Forbidden(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(reply("{}")), &commonv1.Hypothesis{Id: "h1", OwnerId: "alice"})
	if _, err := svc.StoreITC(context.Background(), "bob", "h1", ITCInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign StoreITC must be forbidden, got %v", err)
	}
}

func TestRefine_StrongVerdictReturnsEarly(t *testing.T) {
	supported := reply(`{"verdict":"supported","confidence":0.9,"rationale":"",`+
		`"claims":[{"claim":"alpha beta gamma delta","verdict":"supported","quote":"a b c d"}],`+
		`"supporting":["a b c d"],"contradicting":[]}`,
		&commonv1.Source{ChunkId: "c1", Snippet: "a b c d e"})
	// Проверка — два прохода (основной и встречный); при сильном вердикте переписывания нет.
	a := newAnswerer(supported, supported)
	_, svc := seedHypothesis(a, &commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "alpha beta gamma delta"})
	if _, err := svc.Refine(context.Background(), "alice", "h1"); err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if a.callCount() != 2 {
		t.Fatalf("strong verdict must skip revision, got %d passes", a.callCount())
	}
}

func TestRefine_WeakVerdictRevises(t *testing.T) {
	weak := reply(`{"verdict":"insufficient","confidence":0.4,"rationale":"",` +
		`"supporting":[],"contradicting":["lim"]}`)
	revised := reply(`{"statement":"a sharper, grounded statement"}`)
	// Проходы: проверка (основной, встречный) → переписывание → снова проверка.
	a := newAnswerer(weak, weak, revised, weak, weak)
	cat, svc := seedHypothesis(a, &commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "vague"})
	if _, err := svc.Refine(context.Background(), "alice", "h1"); err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if cat.hypotheses["h1"].GetStatement() != "a sharper, grounded statement" {
		t.Fatalf("Refine must apply the revised statement, got %q", cat.hypotheses["h1"].GetStatement())
	}
}

func TestRefine_VerifyError(t *testing.T) {
	_, svc := seedHypothesis(newAnswerer(failReply(errors.New("down"))),
		&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	if _, err := svc.Refine(context.Background(), "alice", "h1"); err == nil {
		t.Fatal("verify error must surface")
	}
}

func TestWeakVerdict(t *testing.T) {
	for _, v := range []string{verdictRefuted, verdictMixed, verdictInsufficient, ""} {
		if !weakVerdict(v) {
			t.Fatalf("%q should be weak", v)
		}
	}
	if weakVerdict(verdictSupported) {
		t.Fatal("supported is strong")
	}
}

func TestClassifyEvidenceStances(t *testing.T) {
	a := newAnswerer(reply(`[{"i":1,"stance":"supports"},{"i":2,"stance":"contradicts"},{"i":99,"stance":"supports"}]`))
	svc := newHypService(newCatalog(), a)
	ev := []*commonv1.HypothesisEvidence{
		{ChunkId: "c1", Snippet: "frag one", Stance: stanceContext},
		{ChunkId: "c2", Snippet: "frag two", Stance: stanceContext},
	}
	svc.classifyEvidenceStances(context.Background(), "alice", "stmt", ev)
	if ev[0].GetStance() != stanceSupports || ev[1].GetStance() != stanceContradicts {
		t.Fatalf("stances not applied: %q / %q", ev[0].GetStance(), ev[1].GetStance())
	}
}

func TestClassifyEvidenceStances_OutageKeepsContext(t *testing.T) {
	svc := newHypService(newCatalog(), newAnswerer(failReply(errors.New("down"))))
	ev := []*commonv1.HypothesisEvidence{{ChunkId: "c1", Snippet: "f", Stance: stanceContext}}
	svc.classifyEvidenceStances(context.Background(), "alice", "stmt", ev)
	if ev[0].GetStance() != stanceContext {
		t.Fatalf("outage must not change stance, got %q", ev[0].GetStance())
	}
	svc.classifyEvidenceStances(context.Background(), "alice", "stmt", nil)
}

func TestCounterEvidenceSources(t *testing.T) {
	a := newAnswerer(reply("", src("d1", "limitation"), src("d2", "contradiction")))
	svc := newHypService(newCatalog(), a)
	got := svc.counterEvidenceSources(context.Background(), "alice", "statement", 5, nil)
	if len(got) != 2 {
		t.Fatalf("want 2 counter sources, got %d", len(got))
	}
	svc2 := newHypService(newCatalog(), newAnswerer(failReply(errors.New("down"))))
	if got := svc2.counterEvidenceSources(context.Background(), "alice", "s", 5, nil); got != nil {
		t.Fatalf("error must yield nil, got %v", got)
	}
}

func TestGraph_BuildsCitationGraph(t *testing.T) {
	cat := newCatalog()
	cat.list = []*commonv1.Hypothesis{
		{Id: "h1", OwnerId: "alice", Title: "H1", Status: "generated"},
		{Id: "h2", OwnerId: "alice", Title: "H2", Status: "approved"},
	}
	docID := "d1"
	cat.evidence["h1"] = []*commonv1.HypothesisEvidence{
		{DocumentId: &docID, Filename: "shared.pdf", Stance: stanceSupports, ChunkId: "c1"},
	}
	cat.evidence["h2"] = []*commonv1.HypothesisEvidence{
		{DocumentId: &docID, Filename: "shared.pdf", Stance: stanceContradicts, ChunkId: "c2"},
		{Filename: "byname.pdf", Stance: stanceContext, ChunkId: "c3"},
	}
	svc := newHypService(cat, nil)
	g, err := svc.Graph(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}
	// Две гипотезы и два документа: общий d1 и file:byname.pdf.
	if len(g.Nodes) != 4 {
		t.Fatalf("want 4 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 3 {
		t.Fatalf("want 3 edges, got %d", len(g.Edges))
	}
}

func TestGraph_ListError(t *testing.T) {
	cat := newCatalog()
	cat.listHypErr = errors.New("db down")
	svc := newHypService(cat, nil)
	if _, err := svc.Graph(context.Background(), "alice"); err == nil {
		t.Fatal("list error must surface")
	}
}

func TestGraphHypotheses_NilStore(t *testing.T) {
	cat := newCatalog()
	cat.kpis["kpi1"] = &commonv1.KPI{Id: "kpi1", OwnerId: "alice"}
	svc := newHypService(cat, nil)
	got, err := svc.GraphHypotheses(t.Context(), "alice", "kpi1")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil store must yield empty slice, got %v / %v", got, err)
	}
}

// Чужая целевая связь не должна становиться подсказкой выбранной цели.
func TestGraphHypotheses_TargetKPI(t *testing.T) {
	cat := newCatalog()
	cat.kpis["kpi1"] = &commonv1.KPI{Id: "kpi1", OwnerId: "alice", Title: "Strength", Metric: "MPa"}
	graph := newGraph()
	graph.edges["alice"] = []KGEdge{
		{
			Relation: relAffectsProperty, FromType: nodeProcess, FromName: "anneal",
			ToType: nodeProperty, ToName: "hardness", HypothesisID: "hA", KPIID: "other",
		},
		{Relation: relSupportsKPI, FromName: "hardness", ToName: "kpi:other", HypothesisID: "hOther", KPIID: "other"},
		{Relation: relSupportsKPI, FromName: "hardness", ToName: "kpi:kpi1", HypothesisID: "hB", KPIID: "kpi1"},
	}
	svc := NewHypothesisService(cat, nil, nil, nil, nil, graph, nil)
	got, err := svc.GraphHypotheses(t.Context(), "alice", "kpi1")
	if err != nil {
		t.Fatalf("GraphHypotheses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want only the target KPI bridge, got %+v", got)
	}
	if got[0].KPI != "Strength (MPa)" || got[0].FromHypothesis != "hA" || got[0].ViaHypothesis != "hB" {
		t.Fatalf("bridge must lead to the named target KPI, got %+v", got[0])
	}
	cat.kpis["unlinked"] = &commonv1.KPI{Id: "unlinked", OwnerId: "alice", Title: "Other property"}
	if got, err := svc.GraphHypotheses(t.Context(), "alice", "unlinked"); err != nil || len(got) != 0 {
		t.Fatalf("unlinked KPI must have no bridges: %+v / %v", got, err)
	}
	cat.kpis["kpi1"].OwnerId = "bob"
	if got, err := svc.GraphHypotheses(t.Context(), "alice", "kpi1"); !errors.Is(err, ErrForbidden) || len(got) != 0 {
		t.Fatalf("foreign KPI must be rejected: %+v / %v", got, err)
	}
	cat.getKPIErr = errors.New("catalog unavailable")
	if got, err := svc.GraphHypotheses(t.Context(), "alice", "kpi1"); !errors.Is(err, cat.getKPIErr) || len(got) != 0 {
		t.Fatalf("KPI lookup error must surface: %+v / %v", got, err)
	}
}

func TestGraphHypotheses_EmptyGraph(t *testing.T) {
	cat := newCatalog()
	cat.kpis["kpi1"] = &commonv1.KPI{Id: "kpi1", OwnerId: "alice"}
	svc := NewHypothesisService(cat, nil, nil, nil, nil, newGraph(), nil)
	got, err := svc.GraphHypotheses(t.Context(), "alice", "kpi1")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty graph must yield no bridges, got %v / %v", got, err)
	}
}
