package application

import (
	"context"
	"slices"
	"strings"
)

type liveMechanismProfile struct {
	model, revision string
	vector          []float64
}

// Рыночная сводка может упоминать десятки технологий: совпадение таких записей не делает их одной сущностью.
func liveDuplicateSourceIDs(topic LiveTopic) []string {
	if !liveVerifiedMechanism(topic) {
		return slices.Concat(topic.PaperIDs, topic.EvidenceIDs)
	}
	ids := make([]string, 0, len(topic.Qualification.Evidence))
	for _, evidence := range topic.Qualification.Evidence {
		ids = append(ids, evidence.SourceID)
	}
	return ids
}

func liveVerifiedMechanism(topic LiveTopic) bool {
	q := topic.Qualification
	if q == nil || !q.Verified || !q.Technology || !q.Relevant || !q.Coherent ||
		q.Label == "" || q.Mechanism == "" || len(q.Evidence) < liveCandidateMinDocuments {
		return false
	}
	for _, evidence := range q.Evidence {
		if !liveGroundedQualificationEvidence(evidence) {
			return false
		}
	}
	return true
}

// Общая статья может описывать разные способы: измеряем механизмы, а не варианты названий.
func (s *TrendLiveService) profileCandidateMechanisms(ctx context.Context, umbrella *LiveTopic, candidates []LiveTopic) {
	if s.semantic == nil || ctx.Err() != nil || len(candidates) == 0 {
		return
	}
	texts, indexes := []string{}, []int{}
	for i, candidate := range candidates {
		if liveVerifiedMechanism(candidate) {
			texts = append(texts, candidate.Qualification.Mechanism)
			indexes = append(indexes, i)
		}
	}
	if len(texts) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, liveRelevanceWait)
	defer cancel()
	batch, err := s.semantic.Encode(ctx, texts)
	if err == nil {
		err = liveNormalizeSemanticBatch(&batch, len(texts))
	}
	if err != nil {
		umbrella.Limits = append(umbrella.Limits, liveLimitFail(liveLimitAnalysisOff, "candidate mechanisms", err))
		return
	}
	for row, i := range indexes {
		candidates[i].mechanismProfile = &liveMechanismProfile{batch.Model, batch.Revision, batch.Vectors[row]}
	}
}

func liveDifferentMechanisms(a, b LiveTopic) bool {
	if !liveVerifiedMechanism(a) || !liveVerifiedMechanism(b) {
		return false
	}
	x, y := a.mechanismProfile, b.mechanismProfile
	if x == nil || y == nil || x.model != y.model || x.revision != y.revision ||
		len(x.vector) == 0 || len(x.vector) != len(y.vector) || liveSimilarity(x.vector, y.vector) >= liveSemanticJoin {
		return false
	}
	if liveLabelKey(a.Qualification.Mechanism) == liveLabelKey(b.Qualification.Mechanism) {
		return false
	}
	shared := false
	for _, left := range a.Qualification.Evidence {
		for _, right := range b.Qualification.Evidence {
			if left.SourceID != right.SourceID {
				continue
			}
			// Одна и та же цитата под разными именами не подтверждает независимость механизмов.
			xq, yq := liveLabelKey(left.Quote), liveLabelKey(right.Quote)
			if xq == "" || yq == "" || strings.Contains(xq, yq) || strings.Contains(yq, xq) {
				return false
			}
			shared = true
		}
	}
	return shared
}
