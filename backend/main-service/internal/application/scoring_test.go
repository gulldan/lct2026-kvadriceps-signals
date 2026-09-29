package application

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func f64(v float64) *float64 { return &v }
func i32(v int32) *int32     { return &v }

func ev(stance string) *commonv1.HypothesisEvidence {
	return &commonv1.HypothesisEvidence{Stance: stance, Filename: stance + "-paper"}
}

func evDoc(stance, doc string) *commonv1.HypothesisEvidence {
	return &commonv1.HypothesisEvidence{Stance: stance, DocumentId: &doc, Filename: doc + ".pdf"}
}

func TestComputeRanking_ContributionsSumToScore(t *testing.T) {
	r := computeRanking(rankingInputs{
		Novelty:    f64(0.8),
		Risk:       f64(0.2),
		Value:      f64(0.9),
		Confidence: f64(0.8),
		TRL:        i32(7),
		HasKPI:     true,
		Measurable: true,
		Evidence: []*commonv1.HypothesisEvidence{
			evDoc("supports", "a"), evDoc("supports", "b"), evDoc("supports", "c"), evDoc("supports", "d"),
		},
		Verdict: verdictSupported,
	}, DefaultWeights())
	sum := 0.0
	for _, f := range r.Factors {
		sum += f.Contribution
	}
	if math.Abs(sum-r.Score) > 0.01 {
		t.Fatalf("factor contributions %.3f != score %.3f", sum, r.Score)
	}
	if r.Score < 0.7 {
		t.Fatalf("strong hypothesis should rank high, got %.3f", r.Score)
	}
	if len(r.Factors) != 6 {
		t.Fatalf("want 6 explained factors, got %d", len(r.Factors))
	}
}

func TestComputeRanking_ExpertDecisionAdjustsScore(t *testing.T) {
	in := rankingInputs{Novelty: f64(0.6), Value: f64(0.6), Confidence: f64(0.6), HasKPI: true, Measurable: true}
	base := computeRanking(in, DefaultWeights())

	in.Status = statusApproved
	approved := computeRanking(in, DefaultWeights())
	if approved.Score <= base.Score || approved.Expert == nil || approved.Expert.Multiplier != expertApprovedBoost {
		t.Fatalf("approval must boost and record the multiplier: base %.3f, approved %+v", base.Score, approved)
	}

	in.Status = statusRejected
	rejected := computeRanking(in, DefaultWeights())
	if rejected.Score >= base.Score || rejected.Expert == nil {
		t.Fatalf("rejection must cut the score: base %.3f, rejected %+v", base.Score, rejected)
	}
}

// Веса обязаны совпадать с опубликованной формулой: слайд, карточка и API говорят одно и то же.
func TestComputeRanking_WeightsMatchFormula(t *testing.T) {
	r := computeRanking(rankingInputs{}, DefaultWeights())
	want := map[string]float64{
		"kpi_fit": 0.25, "evidence": 0.20, "novelty": 0.20,
		"value": 0.15, "risk_inv": 0.10, "trl_fit": 0.10,
	}
	total := 0.0
	for _, f := range r.Factors {
		if w, ok := want[f.Key]; !ok || math.Abs(w-f.Weight) > 1e-9 {
			t.Fatalf("factor %s weight %.3f, want %.3f", f.Key, f.Weight, want[f.Key])
		}
		total += f.Weight
	}
	if math.Abs(total-1.0) > 1e-9 {
		t.Fatalf("weights must sum to 1, got %.6f", total)
	}
}

// Без оценок признаки нейтральные 0,5 (scored=false), а не ноль из-за NULL: иначе гипотеза проваливается вниз.
func TestComputeRanking_MissingFeaturesNeutral(t *testing.T) {
	r := computeRanking(rankingInputs{}, DefaultWeights())
	if math.Abs(r.Score-0.5) > 0.05 {
		t.Fatalf("all-missing should be ~0.5, got %.3f", r.Score)
	}
	for _, f := range r.Factors {
		if f.Scored {
			t.Fatalf("factor %s should be unscored when no features given", f.Key)
		}
	}
}

func TestEvidenceConfidence_VerdictMovesScore(t *testing.T) {
	base := []*commonv1.HypothesisEvidence{evDoc("supports", "a"), evDoc("supports", "b")}
	supported, _ := evidenceConfidenceFeature(rankingInputs{Evidence: base, Verdict: verdictSupported})
	refuted, _ := evidenceConfidenceFeature(rankingInputs{Evidence: base, Verdict: verdictRefuted})
	if !(supported > refuted) {
		t.Fatalf("supported (%.3f) must exceed refuted (%.3f)", supported, refuted)
	}
	contradicted, _ := evidenceConfidenceFeature(rankingInputs{
		Evidence: []*commonv1.HypothesisEvidence{evDoc("supports", "a"), evDoc("contradicts", "b"), evDoc("contradicts", "c")},
	})
	allsupport, _ := evidenceConfidenceFeature(rankingInputs{
		Evidence: []*commonv1.HypothesisEvidence{evDoc("supports", "a"), evDoc("supports", "b"), evDoc("supports", "c")},
	})
	if !(allsupport > contradicted) {
		t.Fatalf("all-supporting (%.3f) must exceed contradicted (%.3f)", allsupport, contradicted)
	}
}

// Доказательства считаются по источникам, а не по чанкам; источник хоть с одним «против» — противоречащий.
func TestEvidenceCounts_DeduplicatesChunksBySource(t *testing.T) {
	doc := "same-paper"
	sup, con := evidenceCounts([]*commonv1.HypothesisEvidence{
		evDoc("supports", doc),
		evDoc("supports", doc),
		evDoc("contradicts", doc),
		evDoc("supports", "other-paper"),
	})
	if sup != 1 || con != 1 {
		t.Fatalf("evidenceCounts() = supports %d / contradicts %d, want 1 / 1", sup, con)
	}
}

func TestApplyRanking_PersistsBreakdownAndComposite(t *testing.T) {
	h := &commonv1.Hypothesis{
		NoveltyScore: f64(0.6), RiskScore: f64(0.3), ValueScore: f64(0.7), ConfidenceScore: f64(0.65),
		Trl:        i32(5),
		Assessment: `{"check":{"verdict":"supported"},"trl":{"level":5}}`,
		Evidence:   []*commonv1.HypothesisEvidence{ev("supports"), ev("supports")},
	}
	applyRankingWith(h, DefaultWeights())
	if h.CompositeScore == nil {
		t.Fatal("composite_score must be set")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(h.GetAssessment()), &m); err != nil {
		t.Fatalf("assessment must stay valid JSON: %v", err)
	}
	if _, ok := m["ranking"]; !ok {
		t.Fatal("assessment.ranking must be written")
	}
	if _, ok := m["check"]; !ok {
		t.Fatal("existing assessment.check must be preserved")
	}
	if _, ok := m["trl"]; !ok {
		t.Fatal("existing assessment.trl must be preserved")
	}
	var r ranking
	if err := json.Unmarshal(m["ranking"], &r); err != nil {
		t.Fatalf("ranking breakdown must parse: %v", err)
	}
	if math.Abs(r.Score-*h.CompositeScore) > 1e-9 {
		t.Fatalf("composite_score %.3f must mirror ranking.score %.3f", *h.CompositeScore, r.Score)
	}
	if r.Version != rankingVersion {
		t.Fatalf("ranking version = %q, want %q", r.Version, rankingVersion)
	}
}

// Приоритет обучения максимален у ценной, неопределённой (вера ≈ 0,5) и малодоказанной гипотезы;
// applyRanking сохраняет его, не трогая балл и шесть факторов.
func TestLearningPriority_FormulaAndPersistence(t *testing.T) {
	mk := func(value, belief, evidence float64) *commonv1.Hypothesis {
		return &commonv1.Hypothesis{
			ValueScore: f64(value),
			Assessment: `{"scores":{"belief_score":` + ftoa(belief) + `,"evidence_quality":` + ftoa(evidence) + `}}`,
		}
	}
	// ценность 0,9, вера 0,5 (максимум неопределённости), доказательств нет ⇒ 0,9·1·1 = 0,9.
	hot := computeLearningPriority(mk(0.9, 0.5, 0.0))
	if math.Abs(hot.Score-0.9) > 1e-9 {
		t.Fatalf("hot priority = %.3f, want 0.900", hot.Score)
	}
	// Уверенная вера сводит неопределённость к нулю.
	settled := computeLearningPriority(mk(0.9, 0.95, 0.0))
	if !(settled.Score < hot.Score) {
		t.Fatalf("confident belief must lower priority: settled %.3f >= hot %.3f", settled.Score, hot.Score)
	}
	// Сильные доказательства обнуляют множитель (1 − доказательства).
	evidenced := computeLearningPriority(mk(0.9, 0.5, 0.9))
	if !(evidenced.Score < hot.Score) {
		t.Fatalf("strong evidence must lower priority: evidenced %.3f >= hot %.3f", evidenced.Score, hot.Score)
	}

	// Без оценок: вера 0,5, доказательств нет.
	h := mk(0.8, 0.5, 0.0)
	h.Assessment = ""
	applyRankingWith(h, DefaultWeights())
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(h.GetAssessment()), &m); err != nil {
		t.Fatalf("assessment must stay valid JSON: %v", err)
	}
	raw, ok := m["learning_priority"]
	if !ok {
		t.Fatal("assessment.learning_priority must be written")
	}
	if _, ok := m["ranking"]; !ok {
		t.Fatal("assessment.ranking must coexist with learning_priority")
	}
	var lp learningPriority
	if err := json.Unmarshal(raw, &lp); err != nil {
		t.Fatalf("learning_priority must parse: %v", err)
	}
	// ценность 0,8 × неопределённость 1 × (1 − 0) = 0,8.
	if math.Abs(lp.Score-0.8) > 1e-9 {
		t.Fatalf("persisted learning priority = %.3f, want 0.800", lp.Score)
	}
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
