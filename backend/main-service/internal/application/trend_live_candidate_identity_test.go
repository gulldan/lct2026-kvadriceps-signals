package application

import (
	"slices"
	"testing"
)

func TestLiveDedupeRequiresSameVerifiedMechanism(t *testing.T) {
	for _, scenario := range []string{"distinct_mechanisms", "alias", "shared_quote", "missing_measurement",
		"different_revision", "shared_market_only", "same_name"} {
		t.Run(scenario, func(t *testing.T) {
			candidates := []LiveTopic{
				liveIdentityFixture("Квантовый резонатор", "Квантовая фаза преобразуется в смещение резонатора",
					"Quantum phase drives a mechanical resonator", []float64{3, 0}),
				liveIdentityFixture("Алмазный датчик", "Спин дефекта алмазной решётки считывается по флуоресценции",
					"Diamond lattice defects emit spin-dependent fluorescence", []float64{0, 4}),
			}
			encoder := &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: "local", Revision: "pinned",
				Vectors: [][]float64{candidates[0].mechanismProfile.vector, candidates[1].mechanismProfile.vector}}}
			service := &TrendLiveService{semantic: encoder}
			service.profileCandidateMechanisms(t.Context(), &LiveTopic{}, candidates)
			if !slices.Equal(encoder.texts, []string{candidates[0].Qualification.Mechanism, candidates[1].Qualification.Mechanism}) ||
				candidates[0].mechanismProfile.vector[0] != 1 || candidates[1].mechanismProfile.vector[1] != 1 {
				t.Fatal("identity profile is not a normalized mechanism measurement")
			}
			want := 1
			switch scenario {
			case "distinct_mechanisms":
				want = 2
			case "alias":
				candidates[1].mechanismProfile.vector = []float64{.8, .6}
			case "shared_quote":
				candidates[1].Qualification.Evidence[0].Quote = candidates[0].Qualification.Evidence[0].Quote
			case "missing_measurement":
				candidates[1].mechanismProfile = nil
			case "different_revision":
				candidates[1].mechanismProfile.revision = "other"
			case "same_name":
				candidates[1].Label = candidates[0].Label
			case "shared_market_only":
				want = 2
				candidates[1].Qualification.Evidence[0].SourceID = "other-direct-paper"
				candidates[1].PaperIDs = []string{"other-direct-paper"}
				for i := range candidates {
					candidates[i].EvidenceIDs = []string{"market-a", "market-b", "market-c", "market-d"}
				}
			}
			kept, rejected := liveDedupeSelected(candidates, nil)
			if len(kept) != want || len(kept)+len(rejected) != 2 {
				t.Fatalf("incorrect identity decision: kept=%d rejected=%+v", len(kept), rejected)
			}
		})
	}
}

func liveIdentityFixture(label, mechanism, quote string, vector []float64) LiveTopic {
	return LiveTopic{StudyTopic: StudyTopic{PaperIDs: []string{"shared-paper"}}, Name: label, Label: label,
		Qualification: &LiveQualification{Label: label, Mechanism: mechanism, Technology: true, Relevant: true,
			Coherent: true, Verified: true, Evidence: []LiveQualificationEvidence{
				{SourceID: "shared-paper", Quote: quote, SameTechnology: true, Role: "implements",
					IdentityReason: "Описан проверяемый механизм", MechanismQuote: quote,
					RelevanceQuote: quote, RelevanceReason: "Прямо описана требуемая задача"}}},
		mechanismProfile: &liveMechanismProfile{model: "local", revision: "pinned", vector: vector}}
}
