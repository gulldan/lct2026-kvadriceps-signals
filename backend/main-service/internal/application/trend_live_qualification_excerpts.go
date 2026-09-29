package application

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Модель выбирает координаты свидетельства, а текст копирует сервер: свободная цитата превращалась в пересказ.
type liveQualificationSource struct {
	Source   int                  `json:"source"`
	Excerpts []liveIndexedExcerpt `json:"excerpts"`
}

type liveQualificationSelection struct {
	Criteria []string `json:"criteria,omitempty"`
	Source   int      `json:"source"`
	From     int      `json:"from"`
	Through  int      `json:"through"`
}

func liveQualificationSources(documents []LiveQualificationDocument) []liveQualificationSource {
	sources := make([]liveQualificationSource, len(documents))
	for i, document := range documents {
		sources[i] = liveQualificationSource{Source: i + 1, Excerpts: liveIndexedSourceExcerpts(document.Text)}
	}
	return sources
}

func liveReadQualification(out aiclients.GenResponse, documents []LiveQualificationDocument,
	scope *LiveQueryScope,
) (LiveQualification, error) {
	var proposal struct {
		CanonicalName string                       `json:"canonical_name"`
		Scope         *LiveQueryScope              `json:"scope"`
		Label         string                       `json:"label"`
		Mechanism     string                       `json:"mechanism"`
		Technology    bool                         `json:"technology"`
		Relevant      bool                         `json:"relevant"`
		Coherent      bool                         `json:"coherent"`
		Reason        string                       `json:"reason"`
		Evidence      []liveQualificationSelection `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(liveJSONObject(out.Text)), &proposal); err != nil {
		return LiveQualification{}, fmt.Errorf("qualification: invalid response: %w", err)
	}
	if proposal.Scope != nil && !liveSameQueryScope(scope, proposal.Scope) {
		return LiveQualification{}, errors.New("qualification: scope changed")
	}
	qualification := LiveQualification{Scope: liveCloneQueryScope(scope), CanonicalName: proposal.CanonicalName,
		Label: liveProposalLabel(proposal.Label), Mechanism: liveRussianText(proposal.Mechanism),
		Technology: proposal.Technology,
		Relevant:   true, Coherent: proposal.Coherent, Reason: liveRussianText(proposal.Reason),
		Model: out.Model, Method: liveQualificationMethod}
	if qualification.Reason == "" || out.Model == "" {
		return qualification, errors.New("qualification: missing Russian reason or model identity")
	}
	if !qualification.Technology || !qualification.Coherent {
		return qualification, nil
	}
	for _, selection := range proposal.Evidence {
		evidence, err := liveResolveQualificationExcerpt(selection, documents)
		if err != nil {
			return qualification, err
		}
		if err := liveValidateScopeCriteria(scope, selection.Criteria); err != nil {
			return qualification, err
		}
		evidence.ScopeCriteria = selection.Criteria
		qualification.Evidence = append(qualification.Evidence, evidence)
	}
	qualification.Relevant = liveQualificationScopeSatisfied(qualification)
	qualification.CanonicalName = liveLiteralQualificationName(qualification, documents)
	return qualification, nil
}

func liveResolveQualificationExcerpt(selection liveQualificationSelection, documents []LiveQualificationDocument,
) (LiveQualificationEvidence, error) {
	if selection.Source < 1 || selection.Source > len(documents) {
		return LiveQualificationEvidence{}, fmt.Errorf("qualification: absent source number %d", selection.Source)
	}
	document := documents[selection.Source-1]
	parts := liveSourceExcerpts(document.Text)
	if selection.From < 1 || selection.Through < selection.From || selection.Through > len(parts) {
		return LiveQualificationEvidence{}, fmt.Errorf("qualification: invalid excerpt range %d:%d for source %d",
			selection.From, selection.Through, selection.Source)
	}
	quote := strings.Join(parts[selection.From-1:selection.Through], "")
	return LiveQualificationEvidence{SourceID: document.ID, Quote: quote}, nil
}

// liveProposalLabel принимает и английское название: русское имя пишется в конце по итоговой выдаче.
func liveProposalLabel(text string) string {
	if label := liveTechnologyLabel(text); label != "" {
		return label
	}
	name := strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(text), "«»\"'")), " ")
	if utf8.RuneCountInString(name) < 3 || utf8.RuneCountInString(name) > 80 ||
		strings.ContainsFunc(name, unicode.IsControl) {
		return ""
	}
	return name
}

func liveTechnologyLabel(text string) string {
	if label := liveRussianText(text); label != "" {
		return label
	}
	name := strings.Trim(strings.TrimSpace(text), "«»\"'")
	if len(name) < 2 || len(name) > 40 || strings.ContainsFunc(name, unicode.IsSpace) {
		return ""
	}
	// Имя собственное — две заглавные или цифра («GraphRAG», «Qwen3»); «Memory» с заглавной в начале — не имя.
	upper, digits := 0, 0
	for _, letter := range name {
		switch {
		case unicode.IsUpper(letter):
			upper++
		case unicode.IsDigit(letter):
			digits++
		case unicode.Is(unicode.Latin, letter) || letter == '-' || letter == '.':
		default:
			return ""
		}
	}
	if upper < 2 && digits == 0 {
		return ""
	}
	return name
}
