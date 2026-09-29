package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
)

// LiveSemanticPair несёт метку и происхождение, не выведенные из оценок модели; разработочные примеры — не независимая проверка.
type LiveSemanticPair struct {
	ID          string `json:"id"`
	Domain      string `json:"domain"`
	Relation    string `json:"relation"`
	LabelSource string `json:"label_source"`
	Split       string `json:"split"`
	ATitle      string `json:"a_title"`
	AAbstract   string `json:"a_abstract"`
	BTitle      string `json:"b_title"`
	BAbstract   string `json:"b_abstract"`
}

type LivePairScore struct {
	ID                 string   `json:"id"`
	Domain             string   `json:"domain"`
	Relation           string   `json:"relation"`
	LabelSource        string   `json:"label_source"`
	Split              string   `json:"split"`
	TitleSimilarity    float64  `json:"title_similarity"`
	AbstractSimilarity *float64 `json:"abstract_similarity"`
	TitleCorrect       bool     `json:"title_correct"`
	AbstractCorrect    *bool    `json:"abstract_correct"`
}

type LivePairMetrics struct {
	Pairs             int      `json:"pairs"`
	TruePositive      int      `json:"true_positive"`
	TrueNegative      int      `json:"true_negative"`
	FalsePositive     int      `json:"false_positive"`
	FalseNegative     int      `json:"false_negative"`
	Precision         *float64 `json:"precision"`
	Recall            *float64 `json:"recall"`
	FalsePositiveRate *float64 `json:"false_positive_rate"`
	Accuracy          *float64 `json:"accuracy"`
}

type LiveSemanticEvaluation struct {
	Method        string          `json:"method"`
	Model         string          `json:"model"`
	Revision      string          `json:"revision"`
	Threshold     float64         `json:"threshold"`
	QualityStatus string          `json:"quality_status"`
	Title         LivePairMetrics `json:"title"`
	TitleMatched  LivePairMetrics `json:"title_with_available_abstracts"`
	Abstract      LivePairMetrics `json:"title_and_abstract"`
	Scores        []LivePairScore `json:"scores"`
}

func EvaluateLiveSemanticPairs(
	ctx context.Context, encoder LiveSemanticEncoder, pairs []LiveSemanticPair, threshold float64,
) (LiveSemanticEvaluation, error) {
	result := LiveSemanticEvaluation{Method: "paired-title-abstract-evaluation-v1",
		Threshold: threshold, QualityStatus: liveQualityDevelopment, Scores: []LivePairScore{}}
	if encoder == nil || len(pairs) == 0 || len(pairs) > 500 || threshold < 0 || threshold > 1 || math.IsNaN(threshold) {
		return result, errors.New("evaluation requires a model, 1–500 labeled pairs and a fixed threshold in [0,1]")
	}
	texts, seen := []string{}, map[string]bool{}
	for _, pair := range pairs {
		if pair.ID == "" || seen[pair.ID] || pair.Domain == "" || pair.LabelSource == "" || pair.Split == "" ||
			pair.ATitle == "" || pair.BTitle == "" || !slices.Contains([]string{"same_idea", "different_idea"}, pair.Relation) {
			return result, fmt.Errorf("invalid or unlabeled pair %q", pair.ID)
		}
		seen[pair.ID] = true
		texts = append(texts, liveSemanticText(pair.ATitle, ""), liveSemanticText(pair.BTitle, ""))
		if pair.AAbstract != "" && pair.BAbstract != "" {
			texts = append(texts, liveSemanticText(pair.ATitle, pair.AAbstract), liveSemanticText(pair.BTitle, pair.BAbstract))
		}
	}
	batch, err := encoder.Encode(ctx, texts)
	if err != nil {
		return result, fmt.Errorf("encode semantic evaluation pairs: %w", err)
	}
	if batch.Model == "" || batch.Revision == "" || len(batch.Vectors) != len(texts) {
		return result, errors.New("evaluation received incompatible model response")
	}
	for i, vector := range batch.Vectors {
		unit, ok := liveUnitVector(vector)
		if !ok || len(unit) != len(batch.Vectors[0]) {
			return result, errors.New("evaluation received invalid vector")
		}
		batch.Vectors[i] = unit
	}
	result.Model, result.Revision = batch.Model, batch.Revision
	row := 0
	for _, pair := range pairs {
		same := pair.Relation == "same_idea"
		similarity := liveSimilarity(batch.Vectors[row], batch.Vectors[row+1])
		score := LivePairScore{ID: pair.ID, Domain: pair.Domain, Relation: pair.Relation,
			LabelSource: pair.LabelSource, Split: pair.Split,
			TitleSimilarity: similarity, TitleCorrect: (similarity >= threshold) == same}
		livePairRecord(&result.Title, same, similarity >= threshold)
		row += 2
		if pair.AAbstract != "" && pair.BAbstract != "" {
			livePairRecord(&result.TitleMatched, same, similarity >= threshold)
			value := liveSimilarity(batch.Vectors[row], batch.Vectors[row+1])
			score.AbstractSimilarity, score.AbstractCorrect = new(value), new((value >= threshold) == same)
			livePairRecord(&result.Abstract, same, value >= threshold)
			row += 2
		}
		result.Scores = append(result.Scores, score)
	}
	livePairFinalize(&result.Title)
	livePairFinalize(&result.TitleMatched)
	livePairFinalize(&result.Abstract)
	return result, nil
}

func livePairRecord(m *LivePairMetrics, same, predicted bool) {
	m.Pairs++
	switch {
	case same && predicted:
		m.TruePositive++
	case !same && !predicted:
		m.TrueNegative++
	case !same && predicted:
		m.FalsePositive++
	default:
		m.FalseNegative++
	}
}

func livePairFinalize(m *LivePairMetrics) {
	if total := m.TruePositive + m.FalsePositive; total > 0 {
		m.Precision = new(float64(m.TruePositive) / float64(total))
	}
	if total := m.TruePositive + m.FalseNegative; total > 0 {
		m.Recall = new(float64(m.TruePositive) / float64(total))
	}
	if total := m.TrueNegative + m.FalsePositive; total > 0 {
		m.FalsePositiveRate = new(float64(m.FalsePositive) / float64(total))
	}
	if m.Pairs > 0 {
		m.Accuracy = new(float64(m.TruePositive+m.TrueNegative) / float64(m.Pairs))
	}
}
