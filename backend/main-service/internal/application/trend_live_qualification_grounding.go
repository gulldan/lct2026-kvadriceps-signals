package application

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type liveQualificationReviewSource struct {
	ID       string               `json:"id"`
	Excerpts []liveIndexedExcerpt `json:"excerpts"`
}

type liveQualificationCriterion struct {
	Criteria   []string `json:"criteria,omitempty"`
	SourceFact string   `json:"source_fact"`
	ClaimFact  string   `json:"claim_fact"`
	Reason     string   `json:"reason"`
	Supported  *bool    `json:"supported"`
	From       int      `json:"from"`
	Through    int      `json:"through"`
}

// Само совпадение цитаты не подтверждает две независимые вещи: механизм и его применимость к запросу.
func liveGroundQualificationEvidence(evidence LiveQualificationEvidence, assessment liveQualificationIdentity,
	documents []LiveQualificationDocument,
) (LiveQualificationEvidence, error) {
	index := slices.IndexFunc(documents, func(doc LiveQualificationDocument) bool { return doc.ID == evidence.SourceID })
	if index < 0 {
		return evidence, fmt.Errorf("qualification review: absent source %q", evidence.SourceID)
	}
	if _, err := liveQualificationCriterionQuote(assessment.Candidate, index+1, documents); err != nil {
		return evidence, fmt.Errorf("qualification review original candidate: %w", err)
	}
	mechanism, err := liveQualificationCriterionQuote(assessment.Mechanism, index+1, documents)
	if err != nil {
		return evidence, fmt.Errorf("qualification review mechanism: %w", err)
	}
	relevance, err := liveQualificationCriterionQuote(assessment.Relevance, index+1, documents)
	if err != nil {
		return evidence, fmt.Errorf("qualification review relevance: %w", err)
	}
	evidence.MechanismQuote, evidence.RelevanceQuote = mechanism, relevance
	evidence.ScopeCriteria = slices.Clone(assessment.Relevance.Criteria)
	evidence.IdentityReason = liveQualificationCriterionReason(assessment.Candidate) + " " +
		liveQualificationCriterionReason(assessment.Mechanism)
	evidence.RelevanceReason = liveQualificationCriterionReason(assessment.Relevance)
	return evidence, nil
}

func liveQualificationCriterionQuote(criterion liveQualificationCriterion, source int,
	documents []LiveQualificationDocument,
) (string, error) {
	if criterion.Supported == nil || liveRussianText(criterion.Reason) == "" {
		return "", errors.New("criterion verdict or Russian reason missing")
	}
	if liveRussianText(criterion.SourceFact) == "" || liveRussianText(criterion.ClaimFact) == "" {
		return "", errors.New("criterion source or claim fact missing")
	}
	if !*criterion.Supported {
		return "", nil
	}
	evidence, err := liveResolveQualificationExcerpt(liveQualificationSelection{
		Source: source, From: criterion.From, Through: criterion.Through,
	}, documents)
	if err != nil {
		return "", fmt.Errorf("criterion source range: %w", err)
	}
	return evidence.Quote, nil
}

func liveQualificationCriterionReason(criterion liveQualificationCriterion) string {
	return fmt.Sprintf("Источник: %s Утверждение: %s Основание: %s",
		liveRussianText(criterion.SourceFact), liveRussianText(criterion.ClaimFact), liveRussianText(criterion.Reason))
}

func liveGroundedQualificationEvidence(evidence LiveQualificationEvidence) bool {
	return evidence.SameTechnology && liveQualificationDirectRole(evidence.Role) && evidence.SourceID != "" &&
		strings.TrimSpace(evidence.Quote) != "" && strings.TrimSpace(evidence.MechanismQuote) != "" &&
		strings.TrimSpace(evidence.RelevanceQuote) != "" && liveRussianText(evidence.IdentityReason) != "" &&
		liveRussianText(evidence.RelevanceReason) != ""
}
