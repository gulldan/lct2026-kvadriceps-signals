package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func (s *HypothesisService) Refine(ctx context.Context, ownerID, id string) (*commonv1.Hypothesis, error) {
	ctx, cancel := withDeadline(ctx, llmRefineTimeout)
	defer cancel()
	h, err := s.Verify(ctx, ownerID, id) // заодно сохраняет типизированные свидетельства
	if err != nil {
		return nil, err
	}
	if !weakVerdict(checkVerdict(h.GetAssessment())) {
		return h, nil
	}
	// Правка — по возможности: при любом сбое остаётся проверенная гипотеза, Refine целиком не падает.
	revised, _ := s.reviseStatement(ctx, ownerID, h)
	if revised == "" || revised == h.GetStatement() {
		return h, nil
	}
	h.Statement = revised
	rev := &commonv1.HypothesisRevision{
		Action: actionEdited, EditorId: editorSystem,
		Summary: "Уточнено по результатам проверки (мультиагент)",
	}
	if uerr := s.cat.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		return nil, uerr
	}
	if v, verr := s.Verify(ctx, ownerID, id); verr == nil {
		return v, nil
	}
	return s.GetHypothesis(ctx, ownerID, false, id)
}

func weakVerdict(v string) bool {
	switch v {
	case verdictRefuted, verdictMixed, verdictInsufficient, "":
		return true
	default:
		return false
	}
}

func (s *HypothesisService) reviseStatement(
	ctx context.Context, ownerID string, h *commonv1.Hypothesis,
) (string, error) {
	resp, err := s.answerer.Answer(ctx, &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   h.GetStatement(),
		Prompt:  buildRevisePrompt(h),
		TopK:    verifyEvidenceTop,
	})
	if err != nil {
		return "", err
	}
	return parseRevised(resp.GetAnswer())
}

func buildRevisePrompt(h *commonv1.Hypothesis) string {
	var b strings.Builder
	b.WriteString("Гипотеза: «")
	b.WriteString(h.GetStatement())
	b.WriteString("».")
	if cs := contradictingFrom(h.GetAssessment()); len(cs) > 0 {
		b.WriteString(" Возражения, найденные в базе знаний: ")
		for i, c := range cs {
			if i >= 4 {
				break
			}
			b.WriteString("• ")
			b.WriteString(c)
			b.WriteString(" ")
		}
	}
	b.WriteString("\n\nТы — научный редактор. Опираясь ТОЛЬКО на приведённый выше контекст, перепиши " +
		"формулировку гипотезы так, чтобы она оставалась проверяемой, опиралась на факты из контекста и " +
		"снимала или учитывала указанные возражения. Не добавляй чисел, которых нет в контексте.")
	b.WriteString(untrustedContextInstruction)
	b.WriteString(langInstruction)
	b.WriteString("\nВерни " +
		`СТРОГО JSON без markdown: {"statement":"уточнённая формулировка"}`)
	return b.String()
}

func parseRevised(answer string) (string, error) {
	var r struct {
		Statement string `json:"statement"`
	}
	if err := decodeJSONObject(answer, &r, json.Unmarshal); err != nil {
		if errors.Is(err, errNoJSONObject) {
			return "", errors.New("no JSON object in revise output")
		}
		return "", fmt.Errorf("parse revised statement: %w", err)
	}
	return strings.TrimSpace(r.Statement), nil
}

func contradictingFrom(assessment string) []string {
	if assessment == "" {
		return nil
	}
	var m struct {
		Check struct {
			Contradicting []string `json:"contradicting"`
		} `json:"check"`
	}
	if err := json.Unmarshal([]byte(assessment), &m); err != nil {
		return nil
	}
	return m.Check.Contradicting
}
