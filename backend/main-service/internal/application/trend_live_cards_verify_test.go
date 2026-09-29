package application

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
)

func liveCardTestReview(t *testing.T, selections []liveCardSelection, works []string, fields ...string) string {
	t.Helper()
	var review liveCardReview
	for _, field := range fields {
		row := liveCardFieldReview{Field: field, Verdict: "supported",
			ClaimFact: "Карточка описывает тот же механизм и лабораторное испытание.",
			Reason:    "Утверждение сохраняет механизм, условия и результат исходного испытания."}
		if field == liveClaimAdvantage {
			row.Benefit = new(true)
		}
		for _, selection := range selections {
			if selection.Field != field {
				continue
			}
			row.From, row.Through = new(selection.From), new(selection.Through)
			if selection.Source >= 1 && selection.Source <= len(works) {
				row.SourceFact, _ = liveCardQuote("", liveIndexedSourceExcerpts(works[selection.Source-1]), selection.From, selection.Through)
			}
			break
		}
		review.Fields = append(review.Fields, row)
	}
	return liveEncodeCardReview(t, review)
}

func liveEncodeCardReview(t *testing.T, review liveCardReview) string {
	t.Helper()
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Сохранённый ответ ссылался на заголовки, хотя основание описывало тело статьи. Здесь проверяется
// только привязка факта к цитате: дословное совпадение не доказывает весь смысл утверждения.
func TestLiveCardVerifierBindsSourceFactToFinalQuote(t *testing.T) {
	const heading = "SPIRAL: LEARNING TO SEARCH AND AGGREGATE\n\nSPIRAL: LEARNING TO SEARCH AND AGGREGATE\n\n" +
		"[…]\n\nAbstract\n\n[…]\n\n2."
	const body = " Recall that the search traces are optimized via set reinforcement learning, " +
		"which rewards all generations in a set equally based on the model’s ability to generate " +
		"a correct aggregation trace when conditioning on that set."
	const fact = "the search traces are optimized via set reinforcement learning"
	claim := liveCardAssertion{Field: liveClaimSummary,
		Claim: "SPIRAL — это метод обучения языковой модели генерировать набор независимых трасс рассуждений параллельно, " +
			"а затем агрегировать их в финальный ответ, оптимизируя все компоненты сквозным RL на основе награды за финальный ответ.",
		From: 1, Through: 1, sourceID: 1, quote: heading,
		excerpts: []liveIndexedExcerpt{{Index: 1, Text: heading}, {Index: 2, Text: body}}}
	for _, tc := range []struct {
		name, fact string
		part       int
		want       bool
	}{
		{name: "body_fact_under_headings", fact: fact, part: 1},
		{name: "repaired_body_range", fact: fact, part: 2, want: true},
		{name: "whitespace_and_soft_hyphen", fact: "the\nsearch traces are optimized via set rein\u00adforcement\tlearning",
			part: 2, want: true},
		{name: "empty_fact", part: 2},
		{name: "only_ignored_characters", fact: " \u00ad\n", part: 2},
		{name: "translated_fact",
			fact: "SPIRAL обучает модель генерировать и агрегировать трассы рассуждений, оптимизируя сквозным RL.",
			part: 2},
		{name: "changed_negation", fact: strings.Replace(fact, "are optimized", "are not optimized", 1), part: 2},
		{name: "changed_word", fact: strings.Replace(fact, "reinforcement", "supervised", 1), part: 2},
		{name: "discontinuous_fact", fact: "the search traces are optimized equally based on the model’s ability", part: 2},
		// Сохранённые ответы 27.09: формулы arXiv дают «× 2» и невидимые операторы, проверяющий пишет слитно и
		// ставит точку после оборванного предложения или опускает середину многоточием.
		{name: "layout_and_final_period", fact: "the search\u2061traces are optimized via set reinforcement learning.", part: 2,
			want: true},
		{name: "ordered_ellipsis", fact: "Recall that the search traces … rewards all generations in a set equally", part: 2,
			want: true},
		{name: "reordered_ellipsis", fact: "rewards all generations in a set equally … Recall that the search traces", part: 2},
		{name: "ellipsis_part_elsewhere", fact: "Recall that the search traces … LEARNING TO SEARCH", part: 2},
		{name: "ellipsis_scraps", fact: "Recall that ... equally", part: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := liveEncodeCardReview(t, liveCardReview{Fields: []liveCardFieldReview{{Field: liveClaimSummary,
				SourceFact: tc.fact, ClaimFact: claim.Claim, Verdict: "supported", From: new(tc.part), Through: new(tc.part),
				Reason: "Раздел описывает обучение поиску и агрегации с оптимизацией через обучение с подкреплением."}}})
			evidence, err := liveApplyCardVerification([]liveCardAssertion{claim}, raw)
			if err != nil || len(evidence) != 1 || evidence[0].Verified != tc.want ||
				evidence[0].Quote != claim.excerpts[tc.part-1].Text || evidence[0].SourceID != "1" {
				t.Fatalf("source fact escaped final quote: evidence=%+v error=%v", evidence, err)
			}
		})
	}
	// Без пробелов обрезанное число совпало бы с началом другого.
	if liveCardFactInQuote("error falls by 5.", "error falls by 50 %") || !liveCardFactInQuote("by 5.", "by 5 %") {
		t.Fatal("факт с числом совпал с другим числом цитаты")
	}
}

func TestLiveCardVerificationSeparatesQuoteFromEntailment(t *testing.T) {
	const source = "MOSAIC combines handcrafted acoustic features with WavLM through cross-attention. " +
		"Removing the handcrafted branch lowers EER by 0.67 percentage points under LA, " +
		"but raises EER by 0.76 points under PA. A laboratory evaluation used ASVspoof 2021 DF."
	const wrong = "Акустические признаки улучшают EER на 0,67 процентных пункта при PA."
	const correct = "Акустические признаки улучшают EER на 0,76 процентных пункта при PA."
	// Оба числа действительно есть в источнике: отказ должен сохранить отдельный смысловой вердикт.
	if !liveClaimNumbersFit(wrong, source) || !liveClaimNumbersFit(correct, source) {
		t.Fatal("пример больше не воспроизводит подмену условия при существующем числе")
	}
	for _, tc := range []struct {
		name, claim string
		decisions   []liveCardFieldReview
		want        bool
	}{
		{name: "wrong_condition", claim: wrong, decisions: []liveCardFieldReview{
			{Field: liveClaimAdvantage, Verdict: "contradicted", Reason: "Для PA указан эффект 0,76; 0,67 относится к LA."},
		}},
		{name: "correct_condition", claim: correct, want: true, decisions: []liveCardFieldReview{
			{Field: liveClaimAdvantage, Verdict: "supported", Reason: "Удаление ветки повышает EER PA на 0,76 пункта."},
		}},
		{name: "missing_decision", claim: correct},
		{name: "missing_reason", claim: correct, decisions: []liveCardFieldReview{
			{Field: liveClaimAdvantage, Verdict: "supported"},
		}},
		{name: "conflicting_decisions", claim: wrong, decisions: []liveCardFieldReview{
			{Field: liveClaimAdvantage, Verdict: "supported", Reason: "Число встречается в цитате."},
			{Field: liveClaimAdvantage, Verdict: "contradicted", Reason: "Число относится к другому условию."},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := liveCardDraft{Summary: "Ручные акустические признаки объединяются с WavLM через кросс-внимание.",
				Advantage: tc.claim, Example: "Лабораторное испытание проведено на ASVspoof 2021 DF."}
			review := liveCardReview{Fields: []liveCardFieldReview{
				{Field: liveClaimSummary, SourceFact: "MOSAIC combines handcrafted acoustic features with WavLM through cross-attention.",
					ClaimFact: draft.Summary, Verdict: "supported", Reason: "Источник описывает слияние двух представлений."},
				{Field: liveClaimExample, SourceFact: "A laboratory evaluation used ASVspoof 2021 DF.",
					ClaimFact: draft.Example, Reason: "Лабораторный набор данных указан в источнике.", Verdict: "supported"},
			}}
			for i := range tc.decisions {
				tc.decisions[i].SourceFact = "but raises EER by 0.76 points under PA."
				tc.decisions[i].ClaimFact = tc.claim
				tc.decisions[i].Benefit = new(true)
			}
			review.Fields = append(review.Fields, tc.decisions...)
			for i := range review.Fields {
				review.Fields[i].From = new(1)
				review.Fields[i].Through = new(len(liveSourceExcerpts(source)))
			}
			for _, field := range []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample} {
				draft.Evidence = append(draft.Evidence, liveCardSelection{
					Field: field, Source: 1, From: 1, Through: len(liveSourceExcerpts(source)),
				})
			}
			raw, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			gen := &liveCardTestGenerator{draft: string(raw), verified: liveEncodeCardReview(t, review)}
			card, err := NewCandidateCard(gen, func(_ context.Context) bool { return true })(
				liveFixtureScopeContext(t.Context(), "Защита биометрического голоса от подделок"),
				"Защита биометрического голоса от подделок", LiveCardIdentity{
					Label: "MOSAIC", Mechanism: "Ручные акустические признаки объединяются с WavLM через кросс-внимание.",
				}, []string{source})
			if err != nil {
				t.Fatal(err)
			}
			if (card.Advantage != "") != tc.want || card.Summary == "" || card.Example == "" {
				t.Fatalf("перепутан допуск утверждений: %+v", card)
			}
			if liveCardComplete(LiveTopic{PaperIDs: []string{"1"}, Card: &card}) != tc.want {
				t.Fatalf("неверный допуск карточки после проверки: %+v", card)
			}
			if len(gen.requests) != 2 {
				t.Fatalf("добавлено лишнее обращение к модели: %+v", gen.requests)
			}
		})
	}
	claims := liveCardAssertions(liveCardDraft{Advantage: correct, Evidence: []liveCardSelection{
		{Field: liveClaimAdvantage, Source: 1, From: 1, Through: len(liveSourceExcerpts(source))},
	}}, []string{source})
	if evidence, err := liveApplyCardVerification(claims, `{"supported_fields":["advantage"]}`); err == nil {
		t.Fatalf("старый ответ без поэлементной проверки принят: %+v", evidence)
	}
	if evidence, err := liveApplyCardVerification(claims, liveEncodeCardReview(t, liveCardReview{
		Fields: []liveCardFieldReview{{Field: liveClaimAdvantage, Verdict: "supported", Benefit: new(true),
			From: new(1), Through: new(len(liveSourceExcerpts(source))), Reason: "Число есть в цитате."}},
	})); err != nil ||
		evidence[0].Verified {
		t.Fatalf("ответ без сопоставления фактов получил допуск: %+v, %v", evidence, err)
	}
}
