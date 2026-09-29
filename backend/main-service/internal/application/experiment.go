package application

import (
	"context"
	"fmt"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

const experimentEvidenceTop = 10

const actionPlanned = "planned"

// PlanExperiment при сбое разбора или RPC возвращает ошибку, а не сохраняет пустой план.
func (s *HypothesisService) PlanExperiment(ctx context.Context, ownerID, id string) (*commonv1.Hypothesis, error) {
	ctx, cancel := withDeadline(ctx, llmSinglePassTimeout)
	defer cancel()
	h, err := s.GetHypothesis(ctx, ownerID, false, id)
	if err != nil {
		return nil, err
	}
	resp, aerr := s.answerer.Answer(ctx, &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   h.GetStatement(),         // формулировка — для поиска источников по методам
		Prompt:  buildExperimentPrompt(h), // длинная инструкция — только для LLM
		TopK:    experimentEvidenceTop,
	})
	if aerr != nil {
		return nil, aerr
	}
	plan, perr := parseExperimentPlan(resp.GetAnswer())
	if perr != nil {
		return nil, perr
	}
	h.Detail = mergeExperimentPlan(h.GetDetail(), plan, resp.GetModel())
	rev := &commonv1.HypothesisRevision{
		HypothesisId: id, Action: actionPlanned, EditorId: editorSystem,
		Summary: experimentSummary(plan), Patch: "",
	}
	if uerr := s.cat.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		return nil, uerr
	}
	return s.GetHypothesis(ctx, ownerID, false, id)
}

func mergeExperimentPlan(detail string, p experimentPlan, model string) string {
	m := map[string]any{}
	if detail != "" {
		_ = jsonx.Unmarshal([]byte(detail), &m)
	}
	plan := map[string]any{
		"experiment_type":          normalizeExperimentType(p.ExperimentType),
		"sections":                 cleanExperimentSections(p.Sections),
		"materials":                compactSlice(p.Materials),
		"process_parameters":       cleanExperimentParams(p.ProcessParams),
		"characterization_methods": compactSlice(p.Characterization),
		"test_methods":             compactSlice(p.TestMethods),
		"controls":                 compactSlice(p.Controls),
		"success_criteria":         compactSlice(p.SuccessCriteria),
		"estimated_cost":           normalizeCostLevel(p.EstimatedCost),
		"estimated_time":           normalizeTimeScale(p.EstimatedTime),
		"risks":                    compactSlice(p.Risks),
		keyModel:                   model,
		"planned_at":               time.Now().UTC().Format(time.RFC3339),
	}
	m["experiment_plan"] = plan
	b, err := jsonx.Marshal(m)
	if err != nil {
		return detail
	}
	return string(b)
}

func experimentSummary(p experimentPlan) string {
	steps := len(compactSlice(p.Materials)) + len(cleanExperimentParams(p.ProcessParams)) +
		len(compactSlice(p.TestMethods)) + len(cleanExperimentSections(p.Sections))
	if steps == 0 {
		return "План эксперимента составлен"
	}
	return fmt.Sprintf("План эксперимента составлен: %d пунктов", steps)
}
