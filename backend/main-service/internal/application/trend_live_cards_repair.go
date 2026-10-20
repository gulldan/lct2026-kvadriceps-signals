package application

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Один повтор проверяет только исправленный факт или привязку. Успешные поля не переписываются,
// а отказ или истечение срока не уничтожают уже проверенную часть карточки.
func liveRepairCardClaims(ctx context.Context, gen aiclients.Generator, query string, scope *LiveQueryScope,
	identity LiveCardIdentity, draft *liveCardDraft, claims []liveCardAssertion, evidence []LiveClaimEvidence,
) []LiveClaimEvidence {
	if !liveCandidateTimeLeft(ctx, liveCandidateWaveReserve) {
		return evidence
	}
	pending := liveRepairPending(claims, evidence)
	if len(pending) == 0 {
		return evidence
	}
	raw, err := json.Marshal(liveCardPayload{Query: query, Scope: scope, Identity: identity,
		Sources: liveCardReviewSources(pending)})
	if err != nil {
		return evidence
	}
	var repaired []LiveClaimEvidence
	_, err = liveCardCall(ctx, gen, aiclients.GenRequest{System: liveCardVerifySystem + liveQueryScopeInstruction,
		JSONMode: true, Query: string(raw), MaxTokens: 4096, Temperature: 0},
		func(response aiclients.GenResponse) error {
			var readErr error
			repaired, readErr = liveApplyCardVerification(pending, response.Text)
			return readErr
		})
	if err != nil {
		return evidence
	}
	for _, proof := range repaired {
		if !proof.Verified {
			continue
		}
		claimIndex := slices.IndexFunc(pending, func(c liveCardAssertion) bool { return c.Field == proof.Field })
		index := slices.IndexFunc(evidence, func(e LiveClaimEvidence) bool { return e.Field == proof.Field })
		if claimIndex < 0 || index < 0 || !draft.setClaim(proof.Field, pending[claimIndex].Claim) {
			continue
		}
		evidence[index] = proof
	}
	return evidence
}

// Повтор получает исправление проверяющего или прежнее утверждение, если отказ был в разметке, а не в смысле.
func liveRepairPending(claims []liveCardAssertion, evidence []LiveClaimEvidence) []liveCardAssertion {
	var pending []liveCardAssertion
	for _, claim := range claims {
		if claim.Field == liveClaimMarket || claim.Field == liveClaimAdoption {
			continue
		}
		index := slices.IndexFunc(evidence, func(e LiveClaimEvidence) bool { return e.Field == claim.Field })
		if index < 0 || evidence[index].Verified {
			continue
		}
		decision := evidence[index]
		if decision.hasCorrection(claim.Claim) {
			claim.Claim = decision.correction
		} else if !decision.retryable {
			continue
		}
		claim.quote, claim.GroundingIssues = liveCardQuote(claim.Claim, claim.excerpts, claim.From, claim.Through)
		if decision.partsRequired {
			claim.GroundingIssues = append(claim.GroundingIssues, "parts_required")
		}
		pending = append(pending, claim)
	}
	return pending
}

func (e LiveClaimEvidence) hasCorrection(original string) bool {
	return e.correction != "" && e.correction != original && len(strings.Fields(e.correction)) <= 35
}

func (d *liveCardDraft) setClaim(field, value string) bool {
	switch field {
	case liveClaimSummary:
		d.Summary = value
	case liveClaimAdvantage:
		d.Advantage = value
	case liveClaimExample:
		d.Example = value
	case liveClaimStudyLimitations:
		d.StudyLimitations = value
	case LiveClaimReportAssessment:
		d.ReportAssessment = value
	default:
		// Коды рынка и стадии не восстанавливаются свободным текстом.
		return false
	}
	return true
}
