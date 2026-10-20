package application

import (
	"context"
	"slices"
	"time"
)

// Повторное чтение финальной карточки не должно уступать всё время добору сущностей.
const liveFinalCardReserve = liveCandidateWaveReserve

func liveCardSelectionBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) > 3*liveFinalCardReserve {
		deadline = deadline.Add(-liveFinalCardReserve)
		selection, cancel := context.WithDeadline(ctx, deadline)
		return context.WithValue(selection, liveRunDeadlineKey{}, deadline), cancel
	}
	return context.WithCancel(ctx)
}

func liveRequiredCardFields(topic LiveTopic) int {
	return len(liveReadyCardFields(topic))
}

func liveReadyCardFields(topic LiveTopic) []string {
	if topic.Card == nil {
		return nil
	}
	var ready []string
	for _, field := range []struct{ name, text string }{
		{liveClaimSummary, topic.Card.Summary}, {liveClaimAdvantage, topic.Card.Advantage}, {liveClaimExample, topic.Card.Example},
	} {
		proof := liveClaim(topic.Card.Evidence, field.name)
		if field.text != "" && proof != nil &&
			(slices.Contains(topic.PaperIDs, proof.SourceID) || slices.Contains(topic.EvidenceIDs, proof.SourceID)) &&
			(field.name != liveClaimExample || proof.SourceID == topic.Card.ExampleID) {
			ready = append(ready, field.name)
		}
	}
	return ready
}

// Повтор только у финальных пятнадцати: проверка утверждений та же, уже готовое не теряется при отказе.
func (s *TrendLiveService) completeFinalCards(ctx context.Context, query string, candidates []LiveTopic,
	documents []StudyPaper, metadata map[string]LivePaperMetadata,
) {
	if s.card == nil || !liveCandidateTimeLeft(ctx, liveCallReserve) {
		return
	}
	byID := make(map[string]StudyPaper, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	s.eachConcurrently(ctx, min(len(candidates), liveTopCandidates), func(i int) {
		previous := candidates[i]
		if !liveVerifiedMechanism(previous) || liveCardComplete(previous) {
			return
		}
		repaired := previous
		repaired.Limits = slices.Clone(previous.Limits)
		s.writeCard(ctx, query, &repaired, byID, metadata)
		candidates[i] = liveAcceptCardRecovery(previous, repaired, s.classifier != nil)
	})
}

func liveAcceptCardRecovery(previous, repaired LiveTopic, modelRequired bool) LiveTopic {
	// Новый подтверждённый отказ не отбрасывается даже при отсутствии улучшения обязательных полей.
	market := liveMarketMaturity(repaired, liveAbsoluteBar())
	if market.formed || market.mature {
		previous.Market = market.field()
		return previous
	}
	before, after := liveReadyCardFields(previous), liveReadyCardFields(repaired)
	if len(after) <= len(before) {
		return previous
	}
	// Новая независимая проверка должна сохранить каждое уже подтверждённое обязательное поле.
	if slices.ContainsFunc(before, func(field string) bool { return !slices.Contains(after, field) }) {
		return previous
	}
	if !liveCardComplete(repaired) {
		return repaired
	}
	repaired.Limits = slices.DeleteFunc(repaired.Limits, func(limit LiveLimit) bool { return limit.Code == liveLimitCardOff })
	if previous.Shortfall != nil && previous.Shortfall.Code == liveRejectCard &&
		(!modelRequired || liveModelDecision(repaired.ModelScore)) {
		if kept, _ := liveAdmitCards([]LiveTopic{repaired}); len(kept) == 1 {
			repaired = kept[0]
			repaired.Shortfall = nil
		}
	}
	return repaired
}

func liveFinalMarketCheck(candidates []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for _, candidate := range candidates {
		market := liveMarketMaturity(candidate, liveAbsoluteBar())
		switch {
		case market.formed:
			rejected = append(rejected, liveRejection(candidate, liveRejectMarketFormed, market.reason()))
		case market.mature:
			rejected = append(rejected, liveRejection(candidate, liveRejectMature, market.reason()))
		default:
			kept = append(kept, candidate)
		}
	}
	return kept, rejected
}
