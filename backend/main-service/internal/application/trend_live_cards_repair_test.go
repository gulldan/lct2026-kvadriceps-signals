package application

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
)

func TestLiveCardVerifierRepairsOnlyOriginalCitation(t *testing.T) {
	works := []string{
		"Temporal differences detect replayed signals. The proposed detector has 64 parameters. " +
			"In a laboratory trial it rejected replayed signals. Yes.",
		"An unrelated baseline has 99 parameters and requires batch processing.",
	}
	identity := LiveCardIdentity{Label: "Детектор временных разностей", Mechanism: "Временные разности выявляют повтор сигнала."}
	for _, tc := range []struct {
		name, writerMode, decisionMode string
		from, through                  int
		patch                          map[string]any
		want, wantErr                  bool
	}{
		{name: "repair_missing_number", from: 1, through: 1, patch: map[string]any{"from": 1, "through": 2}, want: true},
		{name: "repair_invalid_range", from: 8, through: 9, patch: map[string]any{"from": 1, "through": 2}, want: true},
		{name: "unchanged_valid_quote", from: 1, through: 2, patch: map[string]any{"from": 1, "through": 2}, want: true},
		{name: "changed_source_fact_number", from: 1, through: 2,
			patch: map[string]any{"from": 1, "through": 2, "source_fact": "The proposed detector has 99 parameters."}},
		{name: "missing_final_range_even_when_original_valid", from: 1, through: 2},
		{name: "null_final_range", from: 1, through: 2, patch: map[string]any{"from": nil, "through": nil}},
		{name: "missing_repair", from: 1, through: 1},
		{name: "partial_repair", from: 1, through: 2, patch: map[string]any{"from": 1}},
		{name: "out_of_bounds", from: 1, through: 2, patch: map[string]any{"from": 1, "through": 9}},
		{name: "zero_based_range", from: 1, through: 2, patch: map[string]any{"from": 0, "through": 2}},
		{name: "reversed_range", from: 1, through: 2, patch: map[string]any{"from": 2, "through": 1}},
		{name: "numbers_still_missing", from: 1, through: 2, patch: map[string]any{"from": 3, "through": 3}},
		{name: "short_quote", from: 1, through: 2, patch: map[string]any{"from": 4, "through": 4}},
		{name: "semantic_rejection", from: 1, through: 1,
			patch: map[string]any{"from": 1, "through": 2, "verdict": "contradicted"}},
		{name: "rejection_needs_no_range", from: 1, through: 2, patch: map[string]any{"verdict": "insufficient"}},
		{name: "missing_decision", from: 1, through: 2, decisionMode: "missing"},
		{name: "duplicate_decision", from: 1, through: 2, decisionMode: "duplicate"},
		{name: "source_substitution", from: 1, through: 2, patch: map[string]any{"source": 2}, wantErr: true},
		{name: "claim_substitution", from: 1, through: 2, patch: map[string]any{"claim": "Новое утверждение."}, wantErr: true},
		{name: "verifier_adds_claim", from: 1, through: 2, writerMode: "missing"},
		{name: "ambiguous_writer_source", from: 1, through: 2, writerMode: "duplicate"},
		{name: "writer_source_absent", from: 1, through: 2, writerMode: "invalid_source"},
		{name: "number_only_in_other_source", from: 1, through: 1, writerMode: "other_number",
			patch: map[string]any{"from": 1, "through": 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := liveCardDraft{Summary: "Детектор временных разностей содержит 64 параметра.",
				Advantage: "Временные разности выявляют повтор сигнала.", Example: "Детектор испытан в лаборатории на повторах сигнала.",
				Evidence: []liveCardSelection{
					{Field: liveClaimSummary, Source: 1, From: tc.from, Through: tc.through},
					{Field: liveClaimAdvantage, Source: 1, From: 1, Through: 1},
					{Field: liveClaimExample, Source: 1, From: 3, Through: 3},
				}}
			switch tc.writerMode {
			case "missing":
				draft.Evidence = draft.Evidence[1:]
			case "duplicate":
				draft.Evidence = append(draft.Evidence, liveCardSelection{Field: liveClaimSummary, Source: 2, From: 1, Through: 1})
			case "invalid_source":
				draft.Evidence[0].Source = 3
			case "other_number":
				draft.Summary = "Детектор временных разностей содержит 99 параметров."
			}
			review := []map[string]any{
				liveRepairReview(liveClaimAdvantage, draft.Advantage, "Temporal differences detect replayed signals."),
				liveRepairReview(liveClaimExample, draft.Example, "In a laboratory trial it rejected replayed signals."),
			}
			review[0]["from"], review[0]["through"] = 1, 1
			review[1]["from"], review[1]["through"] = 3, 3
			decision := liveRepairReview(liveClaimSummary, draft.Summary, "The proposed detector has 64 parameters.")
			for key, value := range tc.patch {
				decision[key] = value
			}
			if tc.decisionMode != "missing" {
				review = append(review, decision)
			}
			if tc.decisionMode == "duplicate" {
				review = append(review, decision)
			}
			gen := &liveCardTestGenerator{draft: liveRepairJSON(t, draft),
				verified: liveRepairJSON(t, map[string]any{"fields": review})}
			card, err := NewCandidateCard(gen, func(context.Context) bool { return true })(
				liveFixtureScopeContext(t.Context(), "Обнаружение повторов сигнала"),
				"Обнаружение повторов сигнала", identity, works)
			if (err != nil) != tc.wantErr {
				t.Fatalf("неверная граница ответа проверяющего: %v", err)
			}
			// Сбой формы проверки даёт ровно один повтор с тем же черновиком; исправление диапазона — ни одного.
			calls := 2
			if tc.wantErr {
				calls = 3
			}
			if len(gen.requests) != calls || tc.wantErr && !strings.Contains(gen.requests[2].Query, "отклонён проверкой формы") {
				t.Fatalf("исправление изменило число обращений: %d", len(gen.requests))
			}
			if tc.wantErr {
				return
			}
			if (card.Summary != "") != tc.want || card.Advantage != draft.Advantage || card.Example != draft.Example {
				t.Fatalf("исправление диапазона неверно допустило или потеряло утверждение: %+v", card)
			}
			if tc.want {
				evidence := liveClaim(card.Evidence, liveClaimSummary)
				if card.Summary != draft.Summary || evidence == nil || evidence.SourceID != "1" || !evidence.Verified ||
					evidence.Quote != strings.Join(liveSourceExcerpts(works[0])[:2], "") {
					t.Fatalf("проверяющий изменил исходное утверждение, источник или буквальную цитату: %+v", card)
				}
			}
			if tc.name == "repair_missing_number" || tc.name == "repair_invalid_range" {
				liveCheckRepairPayload(t, gen.requests[1].Query, draft.Summary, identity, works[0], tc.name)
			}
		})
	}
}

func liveRepairReview(field, claim, fact string) map[string]any {
	review := map[string]any{"field": field, "verdict": "supported", "claim_fact": claim,
		"source_fact": fact,
		"reason":      "Сохранены механизм и условия исходного утверждения."}
	if field == liveClaimAdvantage {
		review["benefit"] = true
	}
	return review
}

func TestLiveCardVerifierRequiresBenefit(t *testing.T) {
	const source = "This comparison found no consistent speed advantage for the tested method. " +
		"The compact detector reduces memory use during the same laboratory trial."
	for _, tc := range []struct {
		name, claim string
		benefit     *bool
		want        bool
	}{
		{name: "truthful_absence_of_benefit", claim: "Сравнение не выявило устойчивого преимущества метода по скорости.",
			benefit: new(false)},
		{name: "beneficial_result", claim: "Компактный детектор уменьшает расход памяти в лабораторном испытании.",
			benefit: new(true), want: true},
		{name: "missing_benefit_decision", claim: "Компактный детектор уменьшает расход памяти в лабораторном испытании."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := liveCardDraft{Advantage: tc.claim, Evidence: []liveCardSelection{
				{Field: liveClaimAdvantage, Source: 1, From: 1, Through: 2},
			}}
			review := liveRepairReview(liveClaimAdvantage, tc.claim,
				"The compact detector reduces memory use during the same laboratory trial.")
			review["from"], review["through"] = 1, 2
			if tc.name == "truthful_absence_of_benefit" {
				review["source_fact"] = "This comparison found no consistent speed advantage for the tested method."
			}
			delete(review, "benefit")
			if tc.benefit != nil {
				review["benefit"] = *tc.benefit
			}
			gen := &liveCardTestGenerator{draft: liveRepairJSON(t, draft),
				verified: liveRepairJSON(t, map[string]any{"fields": []map[string]any{review}})}
			card, err := NewCandidateCard(gen, func(context.Context) bool { return true })(
				liveFixtureScopeContext(t.Context(), "Обнаружение повторов сигнала"),
				"Обнаружение повторов сигнала", LiveCardIdentity{Label: "Компактный детектор",
					Mechanism: "Временные разности выявляют повтор сигнала."}, []string{source})
			if err != nil || len(gen.requests) != 2 || (card.Advantage != "") != tc.want {
				t.Fatalf("правдивый факт спутан с преимуществом: %+v, %v", card, err)
			}
		})
	}
}

func liveRepairJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Отдельный формат чтения позволяет прогнать эту же регрессию на старом коде без новых внутренних типов.
func liveCheckRepairPayload(t *testing.T, raw, claim string, identity LiveCardIdentity, work, mode string) {
	t.Helper()
	var payload struct {
		Identity LiveCardIdentity `json:"identity"`
		Sources  []struct {
			Source   int                  `json:"source"`
			Excerpts []liveIndexedExcerpt `json:"excerpts"`
			Claims   []struct {
				Field  string   `json:"field"`
				Claim  string   `json:"claim"`
				Issues []string `json:"grounding_issues"`
			} `json:"claims"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Identity != identity || len(payload.Sources) != 1 || payload.Sources[0].Source != 1 ||
		len(payload.Sources[0].Claims) != 3 {
		t.Fatalf("потеряно исходное утверждение или привязка к источнику: %s", raw)
	}
	var source strings.Builder
	for i, excerpt := range payload.Sources[0].Excerpts {
		if excerpt.Index != i+1 || strings.Count(raw, excerpt.Text) != 1 {
			t.Fatalf("контекст повторяется для каждого поля или потерял нумерацию: %s", raw)
		}
		source.WriteString(excerpt.Text)
	}
	if source.String() != work {
		t.Fatal("проверяющий получил иной источник")
	}
	wantIssue := "missing_numbers"
	if mode == "repair_invalid_range" {
		wantIssue = "invalid_range"
	}
	got := payload.Sources[0].Claims[0]
	if got.Field != liveClaimSummary || got.Claim != claim || !slices.Contains(got.Issues, wantIssue) {
		t.Fatalf("ошибочный диапазон скрыт от проверяющего: %+v", got)
	}
}
