package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
)

func liveCardTestReview(t *testing.T, draft liveCardDraft, works []string, fields ...string) string {
	t.Helper()
	var review liveCardReview
	for _, field := range fields {
		row := liveCardFieldReview{Field: field, Verdict: "supported",
			ClaimFact: "Карточка описывает тот же механизм и лабораторное испытание.",
			Reason:    "Утверждение сохраняет механизм, условия и результат исходного испытания."}
		if field == liveClaimAdvantage {
			row.Benefit = new(true)
		}
		if field == liveClaimExample {
			row.Performed = new(true)
		}
		for _, selection := range draft.Evidence {
			if selection.Field != field {
				continue
			}
			row.From, row.Through = new(selection.From), new(selection.Through)
			if selection.Source >= 1 && selection.Source <= len(works) {
				row.SourceFact, _ = liveCardQuote("", liveIndexedSourceExcerpts(works[selection.Source-1]), selection.From, selection.Through)
			}
			break
		}
		if field == liveClaimSummary || field == liveClaimStudyLimitations {
			row.Parts = []LiveClaimPart{{Claim: draft.claimText(field), SourceFact: row.SourceFact,
				Direct: true, ScopePreserved: true, ModalityPreserved: true}}
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
				Parts: []LiveClaimPart{{Claim: claim.Claim, SourceFact: claim.excerpts[tc.part-1].Text,
					Direct: true, ScopePreserved: true, ModalityPreserved: true}},
				Reason: "Раздел описывает обучение поиску и агрегации с оптимизацией через обучение с подкреплением."}}})
			evidence, err := liveApplyCardVerification([]liveCardAssertion{claim}, raw)
			if err != nil || len(evidence) != 1 || evidence[0].Verified != tc.want ||
				evidence[0].Quote != claim.excerpts[tc.part-1].Text || evidence[0].SourceID != "1" {
				t.Fatalf("source fact escaped final quote: evidence=%+v error=%v", evidence, err)
			}
		})
	}
	// Повтор "from" — самоисправление модели: берётся последнее значение, вся проверка не отбрасывается.
	repeated := strings.Replace(liveEncodeCardReview(t, liveCardReview{Fields: []liveCardFieldReview{{
		Field: liveClaimSummary, SourceFact: fact, ClaimFact: claim.Claim, Verdict: "supported", From: new(2), Through: new(2),
		Parts: []LiveClaimPart{{Claim: claim.Claim, SourceFact: body, Direct: true, ScopePreserved: true,
			ModalityPreserved: true}},
		Reason: "Раздел описывает обучение поиску и агрегации с оптимизацией через обучение с подкреплением."}}}),
		`"from":2`, `"from":1,"from":2`, 1)
	if strings.Count(repeated, `"from":`) != 2 {
		t.Fatalf("fixture lost the repeated member: %s", repeated)
	}
	if evidence, err := liveApplyCardVerification([]liveCardAssertion{claim}, repeated); err != nil ||
		len(evidence) != 1 || !evidence[0].Verified || evidence[0].Quote != body {
		t.Fatalf("repeated from rejected the whole verification: evidence=%+v error=%v", evidence, err)
	}
	// Без пробелов обрезанное число совпало бы с началом другого.
	if !liveCardFactInQuote("AML offers a paradigm shift", "Machine Learning (AML) offers a paradigm shift") ||
		liveCardFactInQuote("AML offers a new paradigm", "Machine Learning (AML) offers a paradigm shift") {
		t.Fatal("скобка вокруг сокращения решила исход сверки вместо слов")
	}
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
					ClaimFact: draft.Summary, Verdict: "supported", Reason: "Источник описывает слияние двух представлений.",
					Parts: []LiveClaimPart{{Claim: draft.Summary,
						SourceFact: "MOSAIC combines handcrafted acoustic features with WavLM through cross-attention.",
						Direct:     true, ScopePreserved: true, ModalityPreserved: true}}},
				{Field: liveClaimExample, SourceFact: "A laboratory evaluation used ASVspoof 2021 DF.",
					ClaimFact: draft.Example, Reason: "Лабораторный набор данных указан в источнике.",
					Verdict: "supported", Performed: new(true)},
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
			calls := 2
			if tc.name == "missing_decision" || tc.name == "conflicting_decisions" {
				calls = 3
			}
			if len(gen.requests) != calls {
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

// GLM 5.3 ставит benefit рядом с fields: в карточке одно преимущество, и флаг относится к нему.
func TestLiveCardVerificationAcceptsTopLevelBenefit(t *testing.T) {
	const source = "MOSAIC combines handcrafted acoustic features with WavLM through cross-attention. " +
		"The fused model lowers EER by 0.67 percentage points on ASVspoof 2021 LA."
	const claim = "Слияние признаков снижает EER на 0,67 процентного пункта на ASVspoof 2021 LA."
	through := len(liveSourceExcerpts(source))
	claims := liveCardAssertions(liveCardDraft{Advantage: claim, Evidence: []liveCardSelection{
		{Field: liveClaimAdvantage, Source: 1, From: 1, Through: through},
	}}, []string{source})
	for _, benefit := range []bool{true, false} {
		raw := fmt.Sprintf(`{"fields":[{"field":"advantage","from":1,"through":%d,`+
			`"source_fact":"The fused model lowers EER by 0.67 percentage points","claim_fact":"Снижение EER на 0,67 пункта",`+
			`"reason":"Источник прямо называет снижение EER","verdict":"supported"}],"benefit":%t}`, through, benefit)
		evidence, err := liveApplyCardVerification(claims, raw)
		if err != nil || len(evidence) != 1 || evidence[0].Verified != benefit {
			t.Fatalf("benefit=%t рядом с fields: %+v, %v", benefit, evidence, err)
		}
	}
}

func TestLiveStudyLimitationsPreserveEveryClauseAndQualifier(t *testing.T) {
	const quote = "We evaluated five formation protocols. The findings may not transfer to industrial production."
	const first = "Проверены пять протоколов;"
	const second = " выводы могут не переноситься на промышленное производство."
	parts := []LiveClaimPart{
		{Claim: first, SourceFact: "We evaluated five formation protocols.",
			Direct: true, ScopePreserved: true, ModalityPreserved: true},
		{Claim: second, SourceFact: "The findings may not transfer to industrial production.",
			Direct: true, ScopePreserved: true, ModalityPreserved: true},
	}
	if !liveClaimPartsFit(first+second, quote, parts) || liveClaimPartsFit(first+second, quote, parts[:1]) {
		t.Fatal("проверка пропустила неподтверждённый хвост или потеряла полный факт")
	}
	for _, tc := range []struct {
		claim string
		parts []string
		want  bool
	}{
		{"Проверены пять протоколов; выводы могут не переноситься на промышленное производство.",
			[]string{"Проверены пять протоколов", "выводы могут не переноситься на промышленное производство"}, true},
		{"Проверены пять протоколов, а выводы могут не переноситься на промышленное производство.",
			[]string{"Проверены пять протоколов", "выводы могут не переноситься на промышленное производство"}, true},
		{"Модели не созданы (возможен отбор), а WES ограничен двумя образцами.",
			[]string{"Модели не созданы", "возможен отбор", "WES ограничен двумя образцами"}, true},
		{"Метод не подходит.", []string{"Метод", "подходит"}, false},
		{"Метод подходит только для этих образцов.", []string{"Метод подходит"}, false},
		{"Один или два протокола.", []string{"Один", "два протокола"}, false},
		{"Срок 1,5 года.", []string{"Срок 1", "5 года"}, false},
		{"Проверены варианты.", []string{"Проверены вар", "анты"}, false},
	} {
		pieces := make([]LiveClaimPart, len(tc.parts))
		for i, piece := range tc.parts {
			pieces[i] = LiveClaimPart{Claim: piece, SourceFact: parts[0].SourceFact,
				Direct: true, ScopePreserved: true, ModalityPreserved: true}
		}
		if liveClaimPartsFit(tc.claim, quote, pieces) != tc.want {
			t.Errorf("неверное покрытие ограничения %q частями %v", tc.claim, tc.parts)
		}
	}
	for _, source := range []string{
		"Organoids were derived from a metastatic adenocarcinoma.", "Organoids were derived from a patient.",
	} {
		if livePatientCountExplicit("Органоиды получены от одного пациента.", source) {
			t.Errorf("размер когорты выведен из единственного числа: %q", source)
		}
	}
	for _, source := range []string{
		"Organoids were derived from one patient.", "Samples came from a single patient.", "Материал получен от одного пациента.",
	} {
		if !livePatientCountExplicit("Органоиды получены от одного пациента.", source) {
			t.Errorf("явный размер когорты потерян: %q", source)
		}
	}
	for _, tc := range []struct {
		claim, source string
		want          bool
	}{
		{"Платформа непригодна для иммунотерапии.", "The platform may not be suitable for immunotherapy.", false},
		{"Платформа может не подойти для иммунотерапии.", "The platform may not be suitable for immunotherapy.", true},
		{"Расчёт не предсказывает фазовую стабильность.",
			"an indicator rather than as a definitive prediction of phase stability", false},
		{"Расчёт не даёт однозначного прогноза фазовой стабильности.",
			"an indicator rather than as a definitive prediction of phase stability", true},
		{"Молекулярное подтипирование не проводилось.", "Molecular subtyping was not performed.", true},
		{"Силу измерить не удалось.", "The force could not be measured under these conditions.", true},
	} {
		if liveLimitationQualifiersFit(tc.claim, tc.source) != tc.want {
			t.Errorf("модальность изменилась: %q / %q", tc.claim, tc.source)
		}
	}
	for _, changed := range []LiveClaimPart{
		{Claim: second, SourceFact: parts[1].SourceFact, ScopePreserved: true, ModalityPreserved: true},
		{Claim: second, SourceFact: parts[1].SourceFact, Direct: true, ModalityPreserved: true},
		{Claim: second, SourceFact: parts[1].SourceFact, Direct: true, ScopePreserved: true},
		{Claim: second, SourceFact: "There were no industrial experiments.",
			Direct: true, ScopePreserved: true, ModalityPreserved: true},
	} {
		if liveClaimPartsFit(first+second, quote, []LiveClaimPart{parts[0], changed}) {
			t.Fatal("косвенный вывод, усиление или выдуманная цитата получили допуск")
		}
	}
	if !liveClaimNumbersFit("Смесь снижает выбросы CO2 на 10%.", "The mixture reduces carbon emissions by 10%.") ||
		liveClaimNumbersFit("Смесь снижает выбросы CO2 на 20%.", "The mixture reduces carbon emissions by 10%.") {
		t.Fatal("индекс химической формулы перепутан с числом результата")
	}
}

func TestSummaryRequiresEvidenceForEveryAlternative(t *testing.T) {
	const quote = "An interphase is formed in situ by decomposition of LiODFB."
	const first = "Межфазный слой формируют разложением LiODFB"
	const second = "либо предварительной обработкой нитратом цинка."
	claim := liveCardAssertion{Field: liveClaimSummary, Claim: first + " либо " + strings.TrimPrefix(second, "либо "),
		sourceID: 1, excerpts: []liveIndexedExcerpt{{Index: 1, Text: quote}}}
	part := LiveClaimPart{Claim: first, SourceFact: quote, Direct: true, ScopePreserved: true, ModalityPreserved: true}
	for _, parts := range [][]LiveClaimPart{nil, {part}, {part, {
		Claim: second, SourceFact: quote, Direct: false, ScopePreserved: true, ModalityPreserved: true,
	}}} {
		review := liveCardFieldReview{Field: liveClaimSummary, Verdict: "supported", From: new(1), Through: new(1),
			SourceFact: quote, ClaimFact: first, Reason: "Первый способ прямо описан", Parts: parts}
		proof := liveCheckedCardClaim(claim, []liveCardFieldReview{review})
		if proof.Verified {
			t.Fatalf("подтверждение одного способа допущено как описание обоих: %+v", proof)
		}
		// Ремонт проверяет узкий пересказ проверяющего, а не повторяет отклонённый перечень.
		if proof.correction != first || !proof.partsRequired {
			t.Fatalf("ремонт не получил подтверждённый способ: %q %v", proof.correction, proof.partsRequired)
		}
	}
	claim.Claim = first
	review := liveCardFieldReview{Field: liveClaimSummary, Verdict: "supported", From: new(1), Through: new(1),
		SourceFact: quote, ClaimFact: first, Reason: "Способ прямо описан", Parts: []LiveClaimPart{part}}
	if proof := liveCheckedCardClaim(claim, []liveCardFieldReview{review}); !proof.Verified {
		t.Fatalf("прямое описание потеряно: %+v", proof)
	}
}

func TestStudyLimitationFormulaKeepsLiteralSourceSpan(t *testing.T) {
	const quote = "The cell used a composite LiNi 0.82 Co 0.10 Mn 0.08 O 2 (NCM) cathode."
	const claim = "Результат получен для ячейки с катодом NCM."
	for _, fact := range []string{
		"composite LiNi0.82Co0.10Mn0.08O2 (NCM) cathode", "composite LiNi 0.82 Co 0.10 Mn 0.08 O 2 (NCM) cathode",
	} {
		part := LiveClaimPart{Claim: claim, SourceFact: fact, Direct: true, ScopePreserved: true, ModalityPreserved: true}
		row := liveCardFieldReview{Field: liveClaimStudyLimitations, SourceFact: quote, ClaimFact: claim,
			Verdict: "supported", Reason: "Сохранён исследованный состав катода", From: new(1), Through: new(1),
			Parts: []LiveClaimPart{part}}
		assertion := liveCardAssertion{Field: liveClaimStudyLimitations, Claim: claim, sourceID: 1,
			excerpts: []liveIndexedExcerpt{{Index: 1, Text: quote}}}
		proof := liveCheckedCardClaim(assertion, []liveCardFieldReview{row})
		literal := strings.Contains(quote, fact)
		if proof.Verified != literal || !literal && !proof.retryable {
			t.Fatalf("запись формулы изменена без повторной проверки: %+v", proof)
		}
	}
}

func TestExampleRequiresSeparateConfirmationOfPerformedEvent(t *testing.T) {
	const planned = "Mercedes-Benz will test ProLogium's Gen4 lithium ceramic cells for safety and fast charging."
	const performed = "In a completed laboratory trial the tested cell retained capacity after repeated cycling."
	for _, test := range []struct {
		quote, claim string
		performed    *bool
		want         bool
	}{
		{planned, "Mercedes-Benz будет тестировать керамические ячейки.", nil, false},
		{planned, "Mercedes-Benz будет тестировать керамические ячейки.", new(false), false},
		{performed, "В лабораторном испытании ячейка сохранила ёмкость после циклирования.", new(true), true},
	} {
		claim := liveCardAssertion{Field: liveClaimExample, Claim: test.claim, sourceID: 1,
			excerpts: []liveIndexedExcerpt{{Index: 1, Text: test.quote}}}
		review := liveCardFieldReview{Field: liveClaimExample, Verdict: "supported", From: new(1), Through: new(1),
			SourceFact: test.quote, ClaimFact: test.claim, Reason: "Текст утверждения совпадает с источником", Performed: test.performed}
		if got := liveCheckedCardClaim(claim, []liveCardFieldReview{review}); got.Verified != test.want {
			t.Fatalf("буквальный пересказ плана подменил проверку состоявшегося опыта: %+v", got)
		}
	}
}

func TestCardNumericGuardRecognizesWholeWrittenNumbers(t *testing.T) {
	for _, test := range []struct {
		claim, quote string
		want         bool
	}{
		{"Исследованы 96 органоидов от 2 пациентов.", "Ninety-six vMOs were generated from two patients.", true},
		{"Исследованы 97 органоидов.", "Ninety-six vMOs were generated.", false},
		{"Исследованы 6 органоидов.", "Ninety-six vMOs were generated.", false},
		{"Исследован 1 органоид.", "One hundred organoids were generated.", false},
		{"Исследованы 6 органоидов.", "One hundred and six organoids were generated.", false},
		{"Доля равна 5.", "The ratio was zero point five.", false},
		{"Доля равна 1.", "One-half of the samples.", false},
		{"Доля равна 2.", "Two-fifths of the samples.", false},
		{"Наблюдение длилось 96 месяцев.", "Ninety-six months of follow-up.", true},
		{"Зафиксированы 2 смерти.", "Two deaths occurred during follow-up.", true},
		{"Доля равна 6.", "Six-tenths of the samples.", false},
		{"Доля равна 1.", "One fifth of the samples.", false},
		{"Доля равна 96.", "Ninety-six/100 of the samples.", false},
		{"Результат равен 6.", "The result was minus six.", false},
		{"Результат равен 6.", "The result was negative six.", false},
		{"Результат равен 6.", "The result was −six.", false},
	} {
		if liveClaimNumbersFit(test.claim, test.quote) != test.want {
			t.Fatalf("словесная запись числа изменила числовой барьер: %q / %q", test.claim, test.quote)
		}
	}
}
