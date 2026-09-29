package application

import "fmt"

// Стадию задаёт только проверенное утверждение карточки с цитатой: ни возраст работы, ни «пилот» в заголовке её не ставят.

const (
	liveAdoptionUnknown   = "unknown"
	liveAdoptionConcept   = "concept"
	liveAdoptionPrototype = "prototype"
	liveAdoptionPilot     = "pilot"
	liveAdoptionEarly     = "early_adoption"
)

type LiveAdoption struct {
	Code string `json:"code"`
	Rank int    `json:"rank"`
	// Matched — источники проверенного утверждения, а не совпадения слов в заголовках.
	Documents int                 `json:"documents"`
	Matched   int                 `json:"matched"`
	Basis     string              `json:"basis"`
	Facts     []string            `json:"facts,omitempty"`
	Evidence  []LiveClaimEvidence `json:"evidence,omitempty"`
}

func liveAdoptionOf(titles []string) *LiveAdoption {
	return liveUnknownAdoption(len(titles))
}

func liveAdoptionMerge(base, extra *LiveAdoption) *LiveAdoption {
	if base == nil {
		return extra
	}
	if extra == nil {
		return base
	}
	return liveUnknownAdoption(base.Documents + extra.Documents)
}

func liveUnknownAdoption(documents int) *LiveAdoption {
	return &LiveAdoption{Code: liveAdoptionUnknown, Documents: documents,
		Basis: fmt.Sprintf("стадия внедрения неизвестна: %d заголовков не заменяют проверенное "+
			"свидетельство о применении именно этой технологии", documents)}
}
