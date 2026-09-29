// Приоритет проверки — дополнительный сигнал рядом с рейтингом, композит из шести факторов он не меняет.
// Выше у ценной, неопределённой и слабо подкреплённой гипотезы: её проверка даст больше всего.

package application

import (
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

type learningPriority struct {
	Score           float64 `json:"score"`
	Value           float64 `json:"value"`
	Uncertainty     float64 `json:"uncertainty"`
	EvidenceQuality float64 `json:"evidence_quality"`
}

func computeLearningPriority(h *commonv1.Hypothesis) learningPriority {
	value := valOr(h.ValueScore)
	belief, evidenceQuality := beliefAndEvidence(h.GetAssessment())
	uncertainty := clamp01(1 - absFloat(belief-0.5)*2)
	score := value * uncertainty * (1 - evidenceQuality)
	return learningPriority{
		Score:           round3(clamp01(score)),
		Value:           round3(value),
		Uncertainty:     round3(uncertainty),
		EvidenceQuality: round3(evidenceQuality),
	}
}

// beliefAndEvidence: без оценок проверки гипотеза считается максимально неопределённой (0,5) и неподкреплённой (0).
func beliefAndEvidence(assessment string) (belief, evidenceQuality float64) {
	belief, evidenceQuality = 0.5, 0
	if assessment == "" {
		return belief, evidenceQuality
	}
	var m struct {
		Scores struct {
			Belief          *float64 `json:"belief_score"`
			EvidenceQuality *float64 `json:"evidence_quality"`
		} `json:"scores"`
	}
	if err := jsonx.Unmarshal([]byte(assessment), &m); err != nil {
		return belief, evidenceQuality
	}
	if m.Scores.Belief != nil {
		belief = clamp01(*m.Scores.Belief)
	}
	if m.Scores.EvidenceQuality != nil {
		evidenceQuality = clamp01(*m.Scores.EvidenceQuality)
	}
	return belief, evidenceQuality
}

func mergeLearningPriority(assessment string, lp learningPriority) string {
	m := map[string]any{}
	if assessment != "" {
		_ = jsonx.Unmarshal([]byte(assessment), &m)
	}
	m["learning_priority"] = lp
	out, err := jsonx.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(out)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
