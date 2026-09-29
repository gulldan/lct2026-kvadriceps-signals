package application

import (
	"context"
	"fmt"
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

const rankingVersion = "rank-v1"

const neutralFeat = 0.5

// ScoringWeights по умолчанию дают в сумме 1; владелец может перенастроить их через ScoringWeightsStore.
type ScoringWeights struct {
	KPIFit   float64 `json:"kpi_fit"`
	Evidence float64 `json:"evidence"`
	Novelty  float64 `json:"novelty"`
	Value    float64 `json:"value"`
	RiskInv  float64 `json:"risk_inv"`
	TRLFit   float64 `json:"trl_fit"`
}

func DefaultWeights() ScoringWeights {
	return ScoringWeights{KPIFit: 0.25, Evidence: 0.20, Novelty: 0.20, Value: 0.15, RiskInv: 0.10, TRLFit: 0.10}
}

// ScoringWeightsStore может быть nil; нет хранилища или записи — действуют DefaultWeights.
type ScoringWeightsStore interface {
	Get(ctx context.Context, ownerID string) (*ScoringWeights, error)
	Set(ctx context.Context, ownerID string, w ScoringWeights) error
}

type rankingInputs struct {
	Novelty    *float64
	Risk       *float64
	Value      *float64
	Confidence *float64
	TRL        *int32
	HasKPI     bool
	Measurable bool
	Evidence   []*commonv1.HypothesisEvidence
	Verdict    string
	Status     string
}

type rankingFactor struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Weight       float64 `json:"weight"`
	Value        float64 `json:"value"`
	Contribution float64 `json:"contribution"`
	Detail       string  `json:"detail"`
	Scored       bool    `json:"scored"`
}

// Решение эксперта — явный множитель поверх суммы, а не скрытая поправка: фронт показывает его отдельной строкой.
const (
	expertApprovedBoost = 1.15
	expertRejectedCut   = 0.4
)

type expertAdjust struct {
	Status     string  `json:"status"`
	Multiplier float64 `json:"multiplier"`
	Label      string  `json:"label"`
}

type ranking struct {
	Score   float64         `json:"score"`
	Factors []rankingFactor `json:"factors"`
	Method  string          `json:"method"`
	Formula string          `json:"formula"`
	Version string          `json:"version"`
	Expert  *expertAdjust   `json:"expert,omitempty"`
}

func expertAdjustFor(status string) *expertAdjust {
	switch status {
	case statusApproved:
		return &expertAdjust{Status: status, Multiplier: expertApprovedBoost, Label: "одобрена экспертом"}
	case statusRejected:
		return &expertAdjust{Status: status, Multiplier: expertRejectedCut, Label: "отклонена экспертом"}
	default:
		return nil
	}
}

// computeRanking: модель только извлекает признаки, итоговый балл считает сервис — так ранжирование
// детерминировано и объяснимо.
func computeRanking(in rankingInputs, w ScoringWeights) ranking {
	kpiFit, kpiScored, kpiNote := kpiFitFeature(in)
	evConf, evNote := evidenceConfidenceFeature(in)
	novelty := valOr(in.Novelty)
	value := valOr(in.Value)
	riskInv := 1 - valOr(in.Risk)
	trlFit, trlScored, trlNote := trlFitFeature(in.TRL)

	factors := []rankingFactor{
		factor("kpi_fit", "Соответствие KPI", w.KPIFit, kpiFit, kpiScored, kpiNote),
		factor("evidence", "Доказательная база", w.Evidence, evConf, hasRankedEvidence(in.Evidence), evNote),
		factor("novelty", "Новизна", w.Novelty, novelty, in.Novelty != nil, scoreNote(in.Novelty)),
		factor("value", "Ожидаемая ценность", w.Value, value, in.Value != nil, scoreNote(in.Value)),
		factor("risk_inv", "Управляемость риска", w.RiskInv, riskInv, in.Risk != nil, riskNote(in.Risk)),
		factor("trl_fit", "Готовность к проверке", w.TRLFit, trlFit, trlScored, trlNote),
	}
	score := 0.0
	for _, f := range factors {
		score += f.Contribution
	}
	expert := expertAdjustFor(in.Status)
	formula := rankingFormula(factors)
	if expert != nil {
		score *= expert.Multiplier
		formula = fmt.Sprintf("(%s) × %.2f·решение эксперта", formula, expert.Multiplier)
	}
	return ranking{
		Score:   round3(clamp01(score)),
		Factors: factors,
		Method:  "weighted-linear",
		Formula: formula,
		Version: rankingVersion,
		Expert:  expert,
	}
}

// rankingFormula строится из текущих весов, чтобы объяснение не врало после перенастройки владельцем.
func rankingFormula(factors []rankingFactor) string {
	parts := make([]string, 0, len(factors))
	for _, f := range factors {
		parts = append(parts, fmt.Sprintf("%.2f·%s", f.Weight, f.Label))
	}
	return strings.Join(parts, " + ")
}

func factor(key, label string, weight, value float64, scored bool, detail string) rankingFactor {
	value = clamp01(value)
	return rankingFactor{
		Key: key, Label: label, Weight: weight, Value: round3(value),
		Contribution: round3(weight * value), Detail: detail, Scored: scored,
	}
}

func kpiFitFeature(in rankingInputs) (float64, bool, string) {
	if in.Confidence == nil && !in.HasKPI && !in.Measurable {
		return neutralFeat, false, "уверенность не оценена"
	}
	conf := valOr(in.Confidence)
	kpiLink := 0.4
	if in.HasKPI {
		kpiLink = 1
	}
	measurable := 0.3
	if in.Measurable {
		measurable = 1
	}
	v := 0.65*conf + 0.20*kpiLink + 0.15*measurable
	parts := []string{fmt.Sprintf("уверенность %d%%", pctInt(conf))}
	if in.HasKPI {
		parts = append(parts, "привязана к KPI")
	} else {
		parts = append(parts, "без KPI")
	}
	if in.Measurable {
		parts = append(parts, "измеримая")
	} else {
		parts = append(parts, "нужна метрика")
	}
	return clamp01(v), true, strings.Join(parts, " · ")
}

func evidenceConfidenceFeature(in rankingInputs) (float64, string) {
	sup, con := evidenceCounts(in.Evidence)
	if sup+con == 0 && in.Verdict == "" {
		return neutralFeat, "доказательства не оценены"
	}

	volume := clamp01(float64(sup) / 4.0)
	balance := float64(sup+1) / float64(sup+con+2) // сглаженная доля «за»
	v := 0.5*volume + 0.5*balance
	switch in.Verdict {
	case verdictSupported:
		v = clamp01(v*0.85 + 0.15)
	case verdictMixed:
		v *= 0.8
	case verdictRefuted:
		v *= 0.35
	case verdictInsufficient:
		v *= 0.7
	}
	note := fmt.Sprintf("%d за / %d против", sup, con)
	if label := verdictLabel(in.Verdict); label != "" {
		note += " · " + label
	}
	return clamp01(v), note
}

func evidenceCounts(evidence []*commonv1.HypothesisEvidence) (supports int, contradicts int) {
	type stances struct {
		supports    bool
		contradicts bool
	}
	bySource := map[string]stances{}
	for _, e := range evidence {
		key := evidenceSourceKey(e)
		if key == "" {
			continue
		}
		s := bySource[key]
		switch e.GetStance() {
		case stanceSupports:
			s.supports = true
		case stanceContradicts:
			s.contradicts = true
		default:
			continue
		}
		bySource[key] = s
	}
	for _, s := range bySource {
		if s.contradicts {
			contradicts++
		} else if s.supports {
			supports++
		}
	}
	return supports, contradicts
}

func hasRankedEvidence(evidence []*commonv1.HypothesisEvidence) bool {
	sup, con := evidenceCounts(evidence)
	return sup+con > 0
}

func evidenceSourceKey(e *commonv1.HypothesisEvidence) string {
	if e.GetDocumentId() != "" {
		return "doc:" + e.GetDocumentId()
	}
	if e.GetFilename() != "" {
		return "file:" + strings.ToLower(strings.TrimSpace(e.GetFilename()))
	}
	if e.GetChunkId() != "" {
		return "chunk:" + e.GetChunkId()
	}
	return ""
}

func trlFitFeature(trl *int32) (float64, bool, string) {
	if trl == nil || *trl < 1 {
		return neutralFeat, false, "готовность не оценена"
	}
	level := int(*trl)
	if level > 9 {
		level = 9
	}
	return clamp01(float64(level-1) / 8.0), true, fmt.Sprintf("УГТ %d/9", level)
}

func rankingFromHypothesis(h *commonv1.Hypothesis, w ScoringWeights) ranking {
	return computeRanking(rankingInputs{
		Novelty:    h.NoveltyScore,
		Risk:       h.RiskScore,
		Value:      h.ValueScore,
		Confidence: h.ConfidenceScore,
		TRL:        h.Trl,
		HasKPI:     h.GetKpiId() != "",
		Measurable: h.GetMeasurable(),
		Evidence:   h.GetEvidence(),
		Verdict:    verdictFromAssessment(h.GetAssessment()),
		Status:     h.GetStatus(),
	}, w)
}

// applyRankingWith — чистый вариант для тестов и сборки генерации; когда владелец известен, нужен
// applyRanking: он подгружает веса владельца.
func applyRankingWith(h *commonv1.Hypothesis, w ScoringWeights) {
	if h == nil {
		return
	}
	r := rankingFromHypothesis(h, w)
	score := r.Score
	h.CompositeScore = &score
	// Приоритет активного обучения — отдельный сигнал, не часть балла: хранится рядом с рейтингом, а не внутри.
	lp := computeLearningPriority(h)
	h.Assessment = mergeLearningPriority(mergeRanking(h.GetAssessment(), r), lp)
}

func mergeRanking(assessment string, r ranking) string {
	m := map[string]any{}
	if assessment != "" {
		_ = jsonx.Unmarshal([]byte(assessment), &m)
	}
	m["ranking"] = r
	out, err := jsonx.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(out)
}

func verdictFromAssessment(assessment string) string {
	if assessment == "" {
		return ""
	}
	var m struct {
		Check struct {
			Verdict string `json:"verdict"`
		} `json:"check"`
	}
	if err := jsonx.Unmarshal([]byte(assessment), &m); err != nil {
		return ""
	}
	return m.Check.Verdict
}

func verdictLabel(verdict string) string {
	switch verdict {
	case verdictSupported:
		return "подтверждается базой"
	case verdictMixed:
		return "частично подтверждается"
	case verdictRefuted:
		return "опровергается базой"
	case verdictInsufficient:
		return "данных недостаточно"
	default:
		return ""
	}
}

func scoreNote(p *float64) string {
	if p == nil {
		return "не оценено"
	}
	return fmt.Sprintf("%d%%", pctInt(clamp01(*p)))
}

func riskNote(p *float64) string {
	if p == nil {
		return "риск не оценён"
	}
	return fmt.Sprintf("риск %d%%", pctInt(clamp01(*p)))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func valOr(p *float64) float64 {
	if p == nil {
		return neutralFeat
	}
	return clamp01(*p)
}

func pctInt(v float64) int { return int(v*100 + 0.5) }

func round3(v float64) float64 { return float64(int(v*1000+0.5)) / 1000 }
