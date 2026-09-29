package application

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

func (s *HypothesisService) persistGenerated(
	ctx context.Context, ownerID string, kpi *commonv1.KPI,
	gen genOutcome, judged map[int]scoreSet, sources []*commonv1.Source, deepPostprocess bool,
	constraints string, works []ExternalWork, inputSet map[string]struct{},
) ([]*commonv1.Hypothesis, error) {
	runID := uuid.NewV4().String()
	if lease := contracts.HypothesisLeaseFrom(ctx); lease.ID != "" {
		runID = lease.ID
	}
	weights := s.rankingWeights(ctx, ownerID)
	type result struct {
		hypothesis *commonv1.Hypothesis
		edges      []KGEdge
		err        error
	}
	results := make([]result, len(gen.items))
	sem := make(chan struct{}, genPersistConcurrency)
	var wg sync.WaitGroup
	for i, item := range gen.items {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if sc, ok := judged[i]; ok {
				item.Novelty, item.Value, item.Risk, item.Confidence = sc.Novelty, sc.Value, sc.Risk, sc.Confidence
			}
			// Новизна считается по плотности предшествующих работ в корпусе и сверке с Materials Project, а не по мнению модели.
			if deepPostprocess {
				nov := s.computeNovelty(ctx, ownerID, item)
				item.Novelty, item.NoveltyRationale = new(nov.Score), nov.Rationale
			}
			evidence := selectEvidence(sources, item)
			markEvidenceOrigin(evidence, inputSet)
			if deepPostprocess {
				s.classifyEvidenceStances(ctx, ownerID, item.Statement, evidence)
			}
			genReq := buildGenRequest(
				ownerID, kpi, item, evidence, runID, gen.resp.GetModel(),
				deepPostprocess, weights, constraints, works,
			)
			writeCtx := ctx
			if lease := contracts.HypothesisLeaseFrom(ctx); lease.ID != "" {
				lease.Step = strconv.Itoa(i)
				writeCtx = contracts.WithHypothesisLease(ctx, lease)
			}
			hyp, cerr := s.cat.CreateHypothesis(writeCtx, genReq)
			if cerr != nil {
				results[i].err = cerr
				return
			}
			results[i].hypothesis = hyp
			results[i].edges = deriveEdges(item, kpi.GetId(), hyp.GetId())
		})
	}
	wg.Wait()
	out := make([]*commonv1.Hypothesis, 0, len(gen.items))
	graphEdges := make([]KGEdge, 0, len(gen.items)*4)
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		out = append(out, result.hypothesis)
		graphEdges = append(graphEdges, result.edges...)
	}
	// Граф — по возможности: сбой или отсутствие хранилища не валит успешную генерацию.
	s.enrichGraph(ctx, ownerID, graphEdges)
	return out, nil
}

func (s *HypothesisService) enrichGraph(ctx context.Context, ownerID string, edges []KGEdge) {
	if s.graph == nil || len(edges) == 0 {
		return
	}
	_ = s.graph.AddEdges(ctx, ownerID, edges)
}

// schemaCompleteness отличает расплывчатые «направления» от проверяемых гипотез: нужны вмешательство,
// механизм, измеримая цель и план проверки.
func schemaCompleteness(item genItem, measurable bool) (float64, []string) {
	checks := []struct {
		ok   bool
		name string
	}{
		{item.MaterialSystem != "" || item.CompositionChange != "" || item.ProcessChange != "", "intervention"},
		{item.MicrostructureMechanism != "" || len(item.CausalChain) > 0, "mechanism"},
		{item.TargetProperty != "" || measurable, "target"},
		{item.Experiment != nil && item.Experiment.SuccessCriteria != "", "validation_plan"},
	}
	missing := make([]string, 0, len(checks))
	for _, c := range checks {
		if !c.ok {
			missing = append(missing, c.name)
		}
	}
	return float64(len(checks)-len(missing)) / float64(len(checks)), missing
}

func buildGenRequest(
	ownerID string, kpi *commonv1.KPI, item genItem,
	evidence []*commonv1.HypothesisEvidence, runID, model string,
	deepPostprocess bool, w ScoringWeights, constraints string, works []ExternalWork,
) *dbv1.CreateHypothesisRequest {
	measurable := true
	if item.Measurable != nil {
		measurable = *item.Measurable
	}
	research := normalizeResearchTag(item.ResearchType)
	if research == "" {
		research = inferResearchTag(
			measurable,
			item.Title,
			item.Statement,
			item.Rationale,
			item.SourceType,
			item.Problem,
			strings.Join(item.Drivers, " "),
		)
	}
	tags := withResearchTag(item.Tags, research)
	var trl *int32
	if item.TRL != nil {
		trl = new(int32(*item.TRL))
	}
	kpiID := kpi.GetId()

	schemaScore, schemaMissing := schemaCompleteness(item, measurable)
	assessmentData := map[string]any{
		"novelty":    dim(item.Novelty, item.NoveltyRationale),
		"risk":       dim(item.Risk, item.RiskRationale),
		"value":      dim(item.Value, item.ValueRationale),
		"confidence": dim(item.Confidence, ""),
		"trl":        map[string]any{keyLevel: item.TRL, keyRationale: item.TRLRationale},
		"schema":     map[string]any{"score": schemaScore, "complete": len(schemaMissing) == 0, keyMissing: schemaMissing},
	}
	if score, rationale, ok := topicFreshness(works, time.Now().Year()); ok {
		assessmentData["topic_freshness"] = dim(score, rationale)
	}
	assessment := mustJSON(assessmentData)
	detail := mustJSON(buildDetail(item))
	tags = dedupeTags(append(tags, "on_demand"))
	inputs := map[string]any{"kpi_id": kpiID}
	if constraints != "" {
		inputs["constraints"] = constraints
	}
	if len(works) > 0 {
		inputs["external_works"] = works
	}
	generationData := map[string]any{
		keyModel: model, "prompt_version": genPromptVer, "kind": "on_demand",
		"inputs":           inputs,
		"deep_postprocess": deepPostprocess,
		"generated_at":     time.Now().UTC().Format(time.RFC3339),
	}
	generation := mustJSON(generationData)

	h := &commonv1.Hypothesis{
		OwnerId: ownerID, RunId: runID, Title: item.Title, Statement: item.Statement,
		Rationale: item.Rationale, Method: "cluster_kpi", Status: statusGenerated, KpiId: &kpiID,
		Trl: trl, NoveltyScore: item.Novelty, RiskScore: item.Risk, ValueScore: item.Value,
		ConfidenceScore: item.Confidence, Measurable: measurable, Organization: item.Organization,
		FunctionArea: item.Function, SourceType: item.SourceType, Location: item.Location,
		Tags: tags, Assessment: assessment, Detail: detail, Generation: generation,
		Evidence: evidence,
	}
	applyRankingWith(h, w)
	return &dbv1.CreateHypothesisRequest{
		Hypothesis: h,
		Initial:    &commonv1.HypothesisRevision{Action: actionCreated, EditorId: editorSystem, Summary: "сгенерировано по KPI"},
	}
}

func mustJSON(v map[string]any) string {
	b, err := jsonx.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func buildDetail(item genItem) map[string]any {
	d := map[string]any{"problem_addressed": item.Problem, "drivers": item.Drivers}
	putIfStr(d, "material_system", compactText(item.MaterialSystem))
	putIfStr(d, "composition_change", compactText(item.CompositionChange))
	putIfStr(d, "process_change", compactText(item.ProcessChange))
	putIfStr(d, "microstructure_mechanism", compactText(item.MicrostructureMechanism))
	putIfStr(d, "target_property", compactText(item.TargetProperty))
	putIfNonEmpty(d, "characterization_methods", compactSlice(item.CharacterizationMethods))
	putIfNonEmpty(d, "test_methods", compactSlice(item.TestMethods))
	putIfNonEmpty(d, "failure_modes", compactSlice(item.FailureModes))
	putIfNonEmpty(d, "causal_chain", cleanCausalChain(item.CausalChain))
	if plan := cleanExperiment(item.Experiment); plan != nil {
		d["experiment_plan"] = plan
	}
	putIfNonEmpty(d, "feasibility", cleanFeasibility(item.Feasibility))
	return d
}

func cleanCausalChain(steps []causalStep) []map[string]string {
	out := make([]map[string]string, 0, len(steps))
	for _, s := range steps {
		stage, change := compactText(s.Stage), compactText(s.Change)
		if stage == "" && change == "" {
			continue
		}
		out = append(out, map[string]string{"stage": stage, "change": change})
	}
	return out
}

func putIfStr(out map[string]any, key, val string) {
	if val != "" {
		out[key] = val
	}
}

func putIfNonEmpty[T any](out map[string]any, key string, val []T) {
	if len(val) > 0 {
		out[key] = val
	}
}

func cleanExperiment(p *experimentVal) map[string]any {
	if p == nil {
		return nil
	}
	horizon := compactText(p.Horizon)
	estimatedTime := normalizeTimeScale(p.EstimatedTime)
	if estimatedTime == "" {
		estimatedTime = normalizeTimeScale(horizon)
	}
	out := map[string]any{}
	putIfStr(out, "experiment_type", normalizeExperimentType(p.ExperimentType))
	putIfNonEmpty(out, "sections", cleanExperimentSections(p.Sections))
	putIfNonEmpty(out, "materials", compactSlice(p.Materials))
	putIfNonEmpty(out, "process_parameters", cleanExperimentParams(p.ProcessParams))
	putIfNonEmpty(out, "characterization_methods", compactSlice(p.Characterization))
	putIfNonEmpty(out, "test_methods", compactSlice(p.TestMethods))
	putIfNonEmpty(out, "controls", compactSlice(p.Controls))
	putIfStr(out, "estimated_cost", normalizeCostLevel(p.EstimatedCost))
	putIfStr(out, "estimated_time", estimatedTime)
	putIfNonEmpty(out, "risks", compactSlice(p.Risks))
	putIfNonEmpty(out, "variables", compactSlice(p.Variables))
	putIfNonEmpty(out, "methods", compactSlice(p.Methods))
	putIfStr(out, "success_criteria", compactText(p.SuccessCriteria))
	putIfStr(out, "horizon", horizon)
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanFeasibility(items []feasibility) []map[string]string {
	out := make([]map[string]string, 0, len(items))
	for _, f := range items {
		aspect, note := compactText(f.Aspect), compactText(f.Note)
		if aspect == "" && note == "" {
			continue
		}
		level := strings.ToLower(compactText(f.Level))
		switch level {
		case levelLow, levelMedium, levelHigh:
		case "низк", "низкий", "низкая":
			level = levelLow
		case "средн", "средний", "средняя":
			level = levelMedium
		case "высок", "высокий", "высокая":
			level = levelHigh
		default:
			level = ""
		}
		out = append(out, map[string]string{"aspect": aspect, "level": level, "note": note})
	}
	return out
}

func compactSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if v := compactText(s); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dim(score any, rationale string) map[string]any {
	return map[string]any{"score": score, keyRationale: rationale}
}
