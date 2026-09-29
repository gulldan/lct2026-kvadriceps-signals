package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Относимость проверяется у каждого кандидата: релевантный в целом корпус содержит и чужие технологии.

const (
	// Центр профиля стабилизируется быстро, а эмбеддинг всех документов дорог.
	liveRelevanceTitles = 12
	liveRelevanceWait   = 60 * time.Second
)

// Сбой измерения не отсекает никого: молчание — не довод против, и ответ говорит, что проверки не было.
func (s *TrendLiveService) candidatesOnTopic(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection) {
	if s.semantic == nil || query == "" || len(candidates) == 0 {
		return candidates, rejected
	}
	texts, inputs := liveRelevanceInputs(candidates, documents)
	if len(inputs) == 0 {
		return candidates, rejected
	}
	// Запрос сравнивается и в русской формулировке, и в английских терминах перевода: работы в окне почти
	// все на английском, и короткий русский запрос близок к ним хуже, чем по смыслу.
	asked := []string{query}
	if terms := strings.Join(umbrella.Query.Terms, "; "); terms != "" {
		asked = append(asked, terms)
	}
	texts = append(texts, asked...)
	ctx, cancel := context.WithTimeout(ctx, liveRelevanceWait)
	defer cancel()
	batch, err := s.semantic.Encode(ctx, texts)
	if err == nil {
		err = liveNormalizeSemanticBatch(&batch, len(texts))
	}
	var profiles [][]float64
	if err == nil {
		profiles, err = liveSemanticCentroids(inputs, batch.Vectors)
	}
	if err != nil {
		umbrella.Limits = append(umbrella.Limits, liveLimitFail(liveLimitRelevanceOff, "candidate relevance", err))
		return candidates, rejected
	}
	queries := batch.Vectors[len(texts)-len(asked):]
	drop := map[int]bool{}
	for i, input := range inputs {
		similarity := 0.0
		for _, vector := range queries {
			similarity = max(similarity, liveSimilarity(profiles[i], vector))
		}
		percent := int(similarity*100 + 0.5)
		value := float64(percent) / 100
		candidates[input.topicIndex].QueryRelevance = &value
		if percent >= liveRelevanceFloor {
			continue
		}
		drop[input.topicIndex] = true
		rejected = append(rejected, liveRejection(candidates[input.topicIndex], liveRejectOffTopic, fmt.Sprintf(
			"технология не о том, что спросили: близость к запросу %d %% при пороге %d %%",
			percent, liveRelevanceFloor)))
	}
	kept := make([]LiveTopic, 0, len(candidates))
	for i := range candidates {
		if !drop[i] {
			kept = append(kept, candidates[i])
		}
	}
	return kept, rejected
}

// Кандидаты делят документы: общий заголовок уходит в модель один раз.
func liveRelevanceInputs(candidates []LiveTopic, documents []StudyPaper) ([]string, []liveSemanticProfile) {
	title := make(map[string]string, len(documents))
	for _, document := range documents {
		title[document.ID] = document.Title
	}
	texts, rows := []string{}, map[string]int{}
	inputs := make([]liveSemanticProfile, 0, len(candidates))
	for i := range candidates {
		input := liveSemanticProfile{topicIndex: i, basis: studyScreeningTitles}
		for _, id := range slices.Concat(candidates[i].PaperIDs, candidates[i].EvidenceIDs) {
			text := title[id]
			if text == "" {
				continue
			}
			row, ok := rows[text]
			if !ok {
				row = len(texts)
				rows[text] = row
				texts = append(texts, text)
			}
			input.rows = append(input.rows, row)
			if len(input.rows) == liveRelevanceTitles {
				break
			}
		}
		if len(input.rows) > 0 {
			inputs = append(inputs, input)
		}
	}
	return texts, inputs
}
