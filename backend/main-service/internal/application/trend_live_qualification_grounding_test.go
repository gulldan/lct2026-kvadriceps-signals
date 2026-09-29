package application

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

func TestQualificationCriteriaCannotBeReplacedByGlobalApproval(t *testing.T) {
	documents := []LiveQualificationDocument{{ID: "study", Text: "The laboratory study detects credit-card fraud. " +
		"A transformer guides a GAN to generate minority transaction samples."}}
	const proposal = `{"label":"Трансформер для генерации редких транзакций",
		"mechanism":"Трансформер направляет генерацию редких транзакций для выявления мошенничества",
		"technology":true,"relevant":true,"coherent":true,"reason":"Описан лабораторный метод проверки транзакций",
		"evidence":[{"source":1,"from":1,"through":2,"criteria":["b1"]}]}`
	for _, scenario := range []string{"single_research_source", "mixed_subsystems", "unproven_domain",
		"missing_verdict", "missing_reason", "missing_source_fact", "missing_claim_fact", "invalid_range", "unproposed_source"} {
		t.Run(scenario, func(t *testing.T) {
			assessment := qualificationIdentityFixture("study", "introduces", true)
			assessment.Mechanism.From, assessment.Mechanism.Through = 2, 2
			wantError, wantCode := false, ""
			switch scenario {
			case "mixed_subsystems":
				assessment.Mechanism.Supported = new(false)
				assessment.Mechanism.Reason = "Операция географической подсистемы ошибочно приписана биометрической"
				wantCode = liveRejectIncoherent
			case "unproven_domain":
				assessment.Relevance.Supported = new(false)
				assessment.Relevance.Reason = "Источник проверяет голосование, перенос на платежи не подтверждён"
				wantCode = liveRejectOffTopic
			case "missing_verdict":
				assessment.Relevance.Supported = nil
				wantError = true
			case "missing_reason":
				assessment.Mechanism.Reason = ""
				wantError = true
			case "missing_source_fact":
				assessment.Mechanism.SourceFact = ""
				wantError = true
			case "missing_claim_fact":
				assessment.Relevance.ClaimFact = " "
				wantError = true
			case "invalid_range":
				assessment.Relevance.Through = 99
				wantError = true
			case "unproposed_source":
				assessment.SourceID = "elsewhere"
				wantError = true
			}
			review, err := json.Marshal(map[string]any{"technology": true, "relevant": true,
				"reason": "Общая оценка положительная", "sources": []liveQualificationIdentity{assessment}})
			if err != nil {
				t.Fatal(err)
			}
			generator := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: proposal, Model: "reader"}, {Text: string(review), Model: "reviewer"}}}
			qualify := NewCandidateQualifier(generator, func(context.Context) bool { return true })
			result, err := qualify(liveFixtureScopeContext(t.Context(), "Антифрод в банковских платежах"),
				"Антифрод в банковских платежах", "transformer GAN", documents)
			accepted := scenario == "single_research_source"
			if (err != nil) != wantError || result.Verified != accepted {
				t.Fatalf("criterion guard failed: verified=%v err=%v qualification=%+v", result.Verified, err, result)
			}
			if !wantError {
				if code, _ := liveQualificationRejection(result); code != wantCode {
					t.Fatalf("rejection code=%q want=%q: %s", code, wantCode, result.Reason)
				}
			}
			if !accepted {
				if !wantError && strings.HasPrefix(result.Reason, "Механизм:") {
					t.Fatal("global approval hid the criterion rejection reason")
				}
				return
			}
			proof := result.Evidence[0]
			parts := liveSourceExcerpts(documents[0].Text)
			if len(result.Evidence) != 1 || proof.MechanismQuote != parts[1] || proof.RelevanceQuote != parts[0] {
				t.Fatalf("criterion citations were not copied from their own source: %+v", proof)
			}
			for reason, criterion := range map[string]liveQualificationCriterion{
				proof.IdentityReason: assessment.Mechanism, proof.RelevanceReason: assessment.Relevance,
			} {
				for _, fact := range []string{criterion.SourceFact, criterion.ClaimFact, criterion.Reason} {
					if !strings.Contains(reason, fact) {
						t.Fatalf("qualification lost its fact comparison: %q missing %q", reason, fact)
					}
				}
			}
			broken := result
			broken.Evidence = slices.Clone(result.Evidence)
			broken.Evidence[0].RelevanceQuote = "Quote copied from an unrelated source"
			if err := liveValidateQualification(broken, documents); err == nil {
				t.Fatal("source-bound validation accepted a forged criterion citation")
			}
			service := NewTrendLiveService(nil, nil)
			detail := LiveDetail{Scope: result.Scope, Candidates: []LiveTopic{{Qualification: &result}}}
			if !service.reusableSearch(detail, "Антифрод в банковских платежах") {
				t.Fatal("current grounded qualification is not reusable")
			}
			result.Method = "grounded-entity-review-v3"
			if service.reusableSearch(detail, "Антифрод в банковских платежах") {
				t.Fatal("legacy qualification bypassed the new criterion review")
			}
			result.Method = liveQualificationMethod
			result.Evidence[0].RelevanceQuote = ""
			if service.reusableSearch(detail, "Антифрод в банковских платежах") || liveVerifiedMechanism(detail.Candidates[0]) {
				t.Fatal("current method without criterion evidence was trusted")
			}
		})
	}
}
