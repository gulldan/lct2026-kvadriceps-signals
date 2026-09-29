package application

import "testing"

func TestLiveAdoptionRequiresGroundedClaim(t *testing.T) {
	base := liveAdoptionOf([]string{
		"Federated Learning for Fraud Detection: A Survey",
		"Federated learning in the wild: healthcare real-world deployment",
		"Prototype of a Privacy-Preserving Scoring Service",
	})
	merged := liveAdoptionMerge(base, liveAdoptionOf([]string{"Первое внедрение скоринговой системы"}))
	for _, stage := range []*LiveAdoption{base, merged, liveAdoptionOf(nil)} {
		if stage.Code != liveAdoptionUnknown || stage.Rank != 0 || stage.Matched != 0 || len(stage.Evidence) != 0 {
			t.Fatalf("заголовок выдан за измерение стадии: %+v", stage)
		}
	}
	if merged.Documents != 4 {
		t.Fatalf("потерян размер просмотренного набора: %+v", merged)
	}
	quote := "A field trial of the named scoring system was conducted at a bank."
	card := liveGroundedCard(liveCardDraft{Adoption: liveAdoptionPilot,
		AdoptionBasis: "Источник описывает банковский пилот этой системы."},
		[]LiveClaimEvidence{{Field: liveClaimAdoption, SourceID: "source", Quote: quote, Verified: true}},
		"writer-test", "verifier-test")
	if card.adoption.Code != liveAdoptionPilot || card.adoption.Rank != 3 || len(card.adoption.Evidence) != 1 {
		t.Fatalf("проверенное основание потеряло стадию: %+v", card.adoption)
	}
}

// Номер работы из запроса к модели на экране указывает не на ту статью: источники там нумеруются заново.
func TestLiveWithoutWorkNumber(t *testing.T) {
	for _, row := range []struct{ in, want string }{
		{"В работе 1 формально верифицируется протокол AMD SEV.",
			"В работе формально верифицируется протокол AMD SEV."},
		{"В работах 2 и 3 показано испытание на стенде.", "В работах показано испытание на стенде."},
		{"Из работы № 4 следует снижение задержки.", "Из работы следует снижение задержки."},
		{"Пилот проведён в сети 5G.", "Пилот проведён в сети 5G."},
		{"В работе 2026 года проверяли устойчивость к подмене голоса.",
			"В работе 2026 года проверяли устойчивость к подмене голоса."},
		{"В работах 2025 и 2026 годов описаны разные методы.",
			"В работах 2025 и 2026 годов описаны разные методы."},
	} {
		if got := liveWithoutWorkNumber(row.in); got != row.want {
			t.Fatalf("номер работы не убран: %q → %q, ожидалось %q", row.in, got, row.want)
		}
	}
}
