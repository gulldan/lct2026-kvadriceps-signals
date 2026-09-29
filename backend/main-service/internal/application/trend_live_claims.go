package application

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Источники — единственное основание фактов, а не предыдущий вердикт о технологии.
type liveCardPayload struct {
	Scope    *LiveQueryScope  `json:"scope"`
	Query    string           `json:"query"`
	Identity LiveCardIdentity `json:"identity"`
	Sources  []liveCardSource `json:"sources,omitempty"`
}

type liveCardSource struct {
	Source   int                  `json:"source"`
	Excerpts []liveIndexedExcerpt `json:"excerpts"`
	Claims   []liveCardAssertion  `json:"claims,omitempty"`
}

func liveCardPrompt(query string, scope *LiveQueryScope, identity LiveCardIdentity, works []string) (string, error) {
	if strings.TrimSpace(identity.Label) == "" || strings.TrimSpace(identity.Mechanism) == "" {
		return "", errors.New("card: missing qualified technology identity")
	}
	sources := make([]liveCardSource, len(works))
	for i, work := range works {
		sources[i].Source, sources[i].Excerpts = i+1, liveIndexedSourceExcerpts(work)
	}
	raw, err := json.Marshal(liveCardPayload{Query: query, Scope: scope, Identity: identity, Sources: sources})
	if err != nil {
		return "", fmt.Errorf("encode card sources: %w", err)
	}
	return string(raw), nil
}

type liveCardAssertion struct {
	Field           string   `json:"field"`
	Claim           string   `json:"claim"`
	From            int      `json:"from"`
	Through         int      `json:"through"`
	GroundingIssues []string `json:"grounding_issues,omitempty"`
	sourceID        int
	quote           string
	excerpts        []liveIndexedExcerpt
}

// Неверный диапазон можно исправить, но неоднозначный или выдуманный источник — нельзя.
func liveCardAssertions(draft liveCardDraft, works []string) []liveCardAssertion {
	counts := make(map[string]int, len(draft.Evidence))
	for _, selection := range draft.Evidence {
		counts[selection.Field]++
	}
	out := make([]liveCardAssertion, 0, len(draft.Evidence))
	for _, selection := range draft.Evidence {
		claim := draft.claimText(selection.Field)
		if counts[selection.Field] != 1 || strings.TrimSpace(claim) == "" ||
			selection.Source < 1 || selection.Source > len(works) {
			continue
		}
		parts := liveIndexedSourceExcerpts(works[selection.Source-1])
		quote, issues := liveCardQuote(claim, parts, selection.From, selection.Through)
		out = append(out, liveCardAssertion{Field: selection.Field, Claim: claim, quote: quote,
			From: selection.From, Through: selection.Through, GroundingIssues: issues,
			sourceID: selection.Source, excerpts: parts})
	}
	return out
}

// Один текст на группу не раздувает проверку длинной статьи числом утверждений карточки.
func liveCardReviewSources(claims []liveCardAssertion) []liveCardSource {
	groups := make([]liveCardSource, 0, len(claims))
	positions := make(map[int]int, len(claims))
	for _, claim := range claims {
		position, found := positions[claim.sourceID]
		if !found {
			position = len(groups)
			positions[claim.sourceID] = position
			groups = append(groups, liveCardSource{Source: claim.sourceID, Excerpts: claim.excerpts})
		}
		groups[position].Claims = append(groups[position].Claims, claim)
	}
	return groups
}

// Дословность и числа проверяются заново после выбора проверяющего, а не наследуются от его вердикта.
func liveCardQuote(claim string, parts []liveIndexedExcerpt, from, through int) (string, []string) {
	if from < 1 || through < from || through > len(parts) {
		return "", []string{"invalid_range"}
	}
	var text strings.Builder
	for _, part := range parts[from-1 : through] {
		text.WriteString(part.Text)
	}
	quote := text.String()
	var issues []string
	if utf8.RuneCountInString(quote) < 20 {
		issues = append(issues, "short_quote")
	}
	if !liveClaimNumbersFit(liveWithoutWorkNumber(claim), quote) {
		issues = append(issues, "missing_numbers")
	}
	return quote, issues
}

func (d liveCardDraft) claimText(field string) string {
	switch field {
	case liveClaimSummary:
		return d.Summary
	case liveClaimAdvantage:
		return d.Advantage
	case liveClaimExample:
		return d.Example
	case LiveClaimReportAssessment:
		return d.ReportAssessment
	case liveClaimMarket:
		return d.Market + ": " + d.MarketBasis
	case liveClaimAdoption:
		return d.Adoption + ": " + d.AdoptionBasis
	}
	return ""
}

var liveClaimNumber = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

// Проверка чисел — дополнительный барьер, а не доказательство смысла.
func liveClaimNumbersFit(claim, quote string) bool {
	numbers := map[float64]bool{}
	for _, raw := range liveClaimNumber.FindAllString(quote, -1) {
		value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err == nil {
			numbers[value] = true
		}
	}
	for _, raw := range liveClaimNumber.FindAllString(claim, -1) {
		value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil || !numbers[value] {
			return false
		}
	}
	return true
}
