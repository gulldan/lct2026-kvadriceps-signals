package application

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Направление карты входит в кандидаты со своим рядом и группами, сохраняет их после проверки сущности и стоит
// выше метода одной статьи; давнее ровное и далёкое от запроса направления отклоняются, а зонтик запроса и
// случайное пересечение кандидатами не становятся.
func TestLiveDirectionCandidatesCarryTrendMeasurement(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 10000
	}
	corpus := make([]StudyPaper, 40)
	for i := range corpus {
		corpus[i] = StudyPaper{ID: fmt.Sprintf("arxiv:q%d", i), Title: fmt.Sprintf("KV cache eviction variant %d", i),
			Date: periods[8+i%6].Start, Authors: []string{fmt.Sprintf("Group %d", i)}}
	}
	ids := func(count int) []string {
		out := make([]string, count)
		for i := range out {
			out[i] = corpus[i].ID
		}
		return out
	}
	direction := func(id string, perMonth []int, shared int) LiveTopic {
		topic := liveHistoryTopic(t, periods, perMonth)
		topic.ID, topic.Name, topic.Query = id, id, LiveQuery{Terms: []string{id + " technology"}}
		topic.PaperIDs = ids(shared)
		return topic
	}
	rising := direction("kv cache", []int{4, 1, 1, 1, 1, 1, 1, 8, 8, 8, 8, 8, 8, 2}, 14)
	flat := direction("old field", []int{5, 5, 5, 5, 5, 5, 5, 1, 1, 1, 1, 1, 1, 0}, 6)
	whole := direction("whole query", []int{4, 1, 1, 1, 1, 1, 1, 8, 8, 8, 8, 8, 8, 2}, 30)
	sparse := direction("chance overlap", []int{4, 1, 1, 1, 1, 1, 1, 8, 8, 8, 8, 8, 8, 2}, 3)
	foreign := direction("fusion energy", []int{4, 1, 1, 1, 1, 1, 1, 8, 8, 8, 8, 8, 8, 2}, 9)
	foreign.Semantic = &LiveSemantic{QuerySimilarity: new(0.18)}
	umbrella := LiveTopic{Name: "сжатие памяти моделей", Source: "search"}

	candidates, rejected := liveDirectionCandidates(umbrella, corpus,
		[]LiveTopic{rising, flat, whole, sparse, foreign}, periods)
	if len(candidates) != 1 || candidates[0].ID != liveDirectionPrefix+"kv cache" {
		t.Fatalf("кандидатом должно стать только растущее направление с общими работами: %v", names(candidates))
	}
	trend := candidates[0]
	if trend.Groups != rising.Groups || trend.PapersCount != rising.PapersCount || trend.Stage != rising.Stage ||
		len(trend.PaperIDs) != liveQualificationDocuments || len(trend.Limits) != 0 {
		t.Fatalf("измерения не взяты у направления: группы %d, работ %d, стадия %q, источников %d",
			trend.Groups, trend.PapersCount, trend.Stage, len(trend.PaperIDs))
	}
	if len(rejected) != 2 || rejected[0].Code != liveRejectHistory || rejected[1].Code != liveRejectOffTopic {
		t.Fatalf("давнее ровное и далёкое от запроса направления не отклонены: %+v", rejected)
	}

	byID := map[string]StudyPaper{}
	for _, paper := range corpus {
		byID[paper.ID] = paper
	}
	cited := trend.PaperIDs[0]
	qualified := liveQualifiedTopic(trend, LiveQualification{Label: "Сжатие KV-кэша",
		Evidence: []LiveQualificationEvidence{{SourceID: cited}}}, byID, periods, liveAbsoluteBar())
	if qualified.Groups != rising.Groups || qualified.PapersCount != rising.PapersCount ||
		!slices.Equal(qualified.PaperIDs, []string{cited}) || !slices.Equal(qualified.Series, rising.Series) {
		t.Fatalf("проверка сущности сузила тренд до цитаты: группы %d, работ %d, источники %v",
			qualified.Groups, qualified.PapersCount, qualified.PaperIDs)
	}

	method := LiveTopic{StudyTopic: StudyTopic{ID: liveExtractPrefix + "one-paper", Groups: 1}, PapersCount: 1,
		ModelScore: &LiveModelScore{Label: liveClassifierWeak, Value: 0.99, Threshold: 0.23}}
	if liveCandidateOrder(qualified, method) >= 0 {
		t.Fatal("метод одной статьи с высокой оценкой модели стоит выше подтверждённого тренда")
	}
}
