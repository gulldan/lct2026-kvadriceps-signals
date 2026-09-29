package application

import (
	"encoding/json/v2"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// Наблюдение из TeleAntiFraud 2.0 доказывает трудность теста, но не банковский эффект внедрения.
// Проверяем весь путь до сохраняемого вердикта, счётчиков свидетельств и уверенности карточки.
func TestVerify_WholeStatementGrounding(t *testing.T) {
	const quote = "Controlled text experiments show that three classifiers achieve perfect macro-averaged F1 " +
		"(Macro-F1) when evaluated against unrelated or ordinary negatives, but drop to 0.65–0.68 " +
		"with near-domain sibling negatives."
	const finding = "На близких отрицательных примерах Macro-F1 текстовых классификаторов падает до 0.65–0.68"
	const effect = "Внедрение TeleAntiFraud 2.0 повысит точность обнаружения мошеннических звонков в банке"
	source := &commonv1.Source{
		DocumentId: "doc", ChunkId: "abstract", Filename: "TeleAntiFraud 2.0.pdf",
		Snippet: strings.NewReplacer("negatives", "nega\u00ad\ntives", "Macro-F1", "Macro\x02-F1", "–", "-").Replace(quote),
	}
	fact := verifyClaim{Claim: finding, Verdict: verdictSupported, Quote: quote, Reason: "Наблюдаемый результат теста"}
	gap := verifyClaim{Claim: effect, Verdict: verdictInsufficient, Reason: "Результаты внедрения в банк не приведены"}
	cases := []struct {
		name      string
		statement string
		verdict   string
		claims    []verifyClaim
		want      string
	}{
		{"unsupported_causal_effect", effect + ", потому что " + finding, verdictSupported,
			[]verifyClaim{fact, gap}, verdictInsufficient},
		{"missing_claim_checks", effect, verdictSupported, nil, verdictInsufficient},
		{"claim_without_quote", effect, verdictSupported,
			[]verifyClaim{{Claim: effect, Verdict: verdictSupported}}, verdictInsufficient},
		{"invented_quote_with_shared_words", effect, verdictSupported,
			[]verifyClaim{fact, {Claim: effect, Verdict: verdictSupported,
				Quote: "TeleAntiFraud 2.0 improves bank detection accuracy and Macro-F1 to 0.95."}}, verdictInsufficient},
		{"insufficient_with_background_support", effect, verdictInsufficient,
			[]verifyClaim{fact, gap}, verdictInsufficient},
		{"observed_finding_supported", finding, verdictSupported, []verifyClaim{fact}, verdictSupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := json.Marshal(verifyResult{
				Verdict: tc.verdict, Confidence: new(0.95), Rationale: "Связанные с темой факты подтверждают гипотезу",
				Claims: tc.claims, Supporting: []string{quote},
			})
			if err != nil {
				t.Fatal(err)
			}
			a := newAnswerer(reply(string(response), source), reply(`{"verdict":"insufficient"}`))
			old := &commonv1.HypothesisEvidence{
				DocumentId: new("doc"), ChunkId: "abstract", Filename: source.GetFilename(), Snippet: quote,
				Stance: stanceSupports, Relation: "Повышает точность в банке",
			}
			cat, svc := seedHypothesis(a, &commonv1.Hypothesis{
				Id: "h", OwnerId: "alice", Statement: tc.statement,
				Evidence: []*commonv1.HypothesisEvidence{old, {
					DocumentId: new("previous-doc"), ChunkId: "old", Filename: "previous.pdf", Snippet: "Old evidence",
					Stance: stanceSupports, Relation: "Старая поддержка",
				}},
				Assessment: `{"check":{"verdict":"supported","confidence":0.95}}`, ConfidenceScore: new(0.95),
			})
			got, err := svc.Verify(t.Context(), "alice", "h")
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			var assessment struct {
				Check  verifyResult     `json:"check"`
				Scores confidenceScores `json:"scores"`
			}
			if err := json.Unmarshal([]byte(cat.hypotheses["h"].GetAssessment()), &assessment); err != nil {
				t.Fatal(err)
			}
			if assessment.Check.Verdict != tc.want || len(assessment.Check.Claims) != len(tc.claims) {
				t.Fatalf("persisted check = %+v; want %s and complete claim checks", assessment.Check, tc.want)
			}
			sup, con := evidenceCounts(got.GetEvidence())
			if tc.want == verdictInsufficient {
				if sup != 0 || con != 0 || len(assessment.Check.Supporting) != 0 || !assessment.Scores.Unverified {
					t.Fatalf("unproved effect retained support: check=%+v, counts=%d/%d", assessment.Check, sup, con)
				}
				if got.GetConfidenceScore() > 0.3 {
					t.Fatalf("unproved effect inherited high confidence: %v", got.GetConfidenceScore())
				}
			} else if sup != 1 || con != 0 || got.GetConfidenceScore() != 0.95 || assessment.Scores.Unverified {
				t.Fatalf("directly supported finding lost support: counts=%d/%d, scores=%+v", sup, con, assessment.Scores)
			}
			if len(got.GetEvidence()) != 2 || old.GetStance() != stanceSupports {
				t.Fatalf("source context must remain without mutating the earlier evidence: %+v", got.GetEvidence())
			}
			for _, e := range got.GetEvidence() {
				if e.GetStance() == stanceContext && e.GetRelation() != "" {
					t.Fatalf("context retained an outdated causal interpretation: %q", e.GetRelation())
				}
			}
		})
	}
}
