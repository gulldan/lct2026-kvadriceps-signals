package application

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestFinalCardRecoveryRequiresIndependentProofAndKeepsReadyFields(t *testing.T) {
	const query = "квантовое измерение"
	for _, supported := range []bool{true, false} {
		service, candidates, documents, _ := liveWaveFixture(t, 1)
		service.SetWorkers(func(context.Context, string, int) int { return 1 })
		candidate := &candidates[0]
		qualification, err := service.qualify(t.Context(), query, candidate.Name,
			[]LiveQualificationDocument{{ID: documents[0].ID, Text: documents[0].Title}})
		if err != nil {
			t.Fatal(err)
		}
		candidate.Qualification = &qualification
		candidate.Label = qualification.Label
		works := []string{liveWorkLine(documents[0], LivePaperMetadata{})}
		draft := liveCardDraft{Summary: "Квантовый резонатор измеряет поле.",
			Advantage: "Резонатор повышает чувствительность измерения.", Example: "Проведено лабораторное испытание резонатора."}
		fields := []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample}
		for _, field := range fields {
			draft.Evidence = append(draft.Evidence, liveCardSelection{Field: field, Source: 1, From: 1, Through: 1})
		}
		claims := liveCardAssertions(draft, works)
		proof, err := liveApplyCardVerification(claims, liveCardTestReview(t, draft, works, fields...))
		if err != nil {
			t.Fatal(err)
		}
		old := liveGroundedCard(draft, proof[:2], "original", "original-check")
		liveRemapClaimSources(old.Evidence, candidate.PaperIDs)
		candidate.Card = &old
		candidate.Shortfall = &LiveShortfall{Code: liveRejectCard, Reason: "Кейс не проверен"}
		if liveCardComplete(*candidate) || liveRequiredCardFields(*candidate) != 2 {
			t.Fatal("неполный исходный пример")
		}
		verified := liveCardTestReview(t, draft, works, fields...)
		if !supported {
			var review liveCardReview
			for _, field := range fields[:2] {
				review.Fields = append(review.Fields, liveCardFieldReview{Field: field, Verdict: "supported",
					SourceFact: works[0], ClaimFact: "Факт совпадает", Reason: "Механизм подтверждён",
					Parts: []LiveClaimPart{{Claim: draft.claimText(field), SourceFact: works[0],
						Direct: true, ScopePreserved: true, ModalityPreserved: true}},
					From: new(1), Through: new(1), Benefit: new(true)})
			}
			review.Fields = append(review.Fields, liveCardFieldReview{Field: liveClaimExample,
				Verdict: "insufficient", Reason: "Источник сообщает план испытания, а не проведённый опыт"})
			verified = liveEncodeCardReview(t, review)
		}
		gen := &liveCardTestGenerator{draft: liveRepairJSON(t, draft), verified: verified}
		service.SetCardWriter(NewCandidateCard(gen, func(context.Context) bool { return true }))
		ctx, cancel := context.WithTimeout(liveFixtureScopeContext(t.Context(), query), liveSearchBudget)
		selection, stop := liveCardSelectionBudget(ctx)
		selectionDeadline, _ := selection.Deadline()
		deadline, _ := ctx.Deadline()
		if deadline.Sub(selectionDeadline) != liveFinalCardReserve {
			t.Fatal("не сохранён бюджет финального чтения")
		}
		if selection.Value(liveRunDeadlineKey{}) != selectionDeadline {
			t.Fatal("запрет поздних вызовов не учитывает резерв финального чтения")
		}
		stop()
		service.completeFinalCards(ctx, query, candidates, documents, nil)
		cancel()
		if len(gen.requests) != 2 || liveCardComplete(candidates[0]) != supported {
			t.Fatalf("повтор обошёл независимую проверку: supported=%v calls=%d card=%+v",
				supported, len(gen.requests), candidates[0].Card)
		}
		if supported {
			if candidates[0].Shortfall != nil || candidates[0].Card.ExampleID != documents[0].ID {
				t.Fatal("допуск не пересчитан или цитата потеряла источник")
			}
		} else if candidates[0].Card != &old || !slices.Equal(candidates[0].Card.Missing, old.Missing) {
			t.Fatal("отказ дополнительной проверки уничтожил готовые поля")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	selection, stop := liveCardSelectionBudget(ctx)
	defer stop()
	a, _ := ctx.Deadline()
	b, _ := selection.Deadline()
	if !a.Equal(b) {
		t.Fatal("короткий срок урезан до невозможности завершить исходные проверки")
	}
}

func TestFinalCardRecoveryKeepsRussianTextNotOnlyItsProof(t *testing.T) {
	proof := make([]LiveClaimEvidence, 0, 3)
	proof = append(proof, LiveClaimEvidence{Field: liveClaimSummary, SourceID: "1",
		Quote: "A tested quantum resonator.", Verified: true})
	old := liveGroundedCard(liveCardDraft{Summary: "Квантовый резонатор измеряет поле."}, proof, "writer", "verifier")
	previous := LiveTopic{PaperIDs: []string{"1"}, Card: &old}
	for _, field := range []string{liveClaimAdvantage, liveClaimExample} {
		proof = append(proof, LiveClaimEvidence{Field: field, SourceID: "1", Quote: "A tested quantum resonator.", Verified: true})
	}
	replacement := liveGroundedCard(liveCardDraft{Summary: "A quantum resonator measures the field.",
		Advantage: "Резонатор повышает чувствительность измерения.", Example: "Резонатор испытан в лаборатории."},
		proof, "writer", "verifier")
	repaired := previous
	repaired.Card = &replacement
	if liveRequiredCardFields(previous) != 1 || liveRequiredCardFields(repaired) != 2 ||
		liveClaim(replacement.Evidence, liveClaimSummary) == nil {
		t.Fatal("пример не воспроизвёл потерю русского текста при сохранении цитаты")
	}
	if got := liveAcceptCardRecovery(previous, repaired, false); got.Card != previous.Card {
		t.Fatal("повтор потерял проверенное русское описание ради двух других полей")
	}
}
