package application

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// Интерфейс переводит оговорку по коду и числам из Values, а не разбирает английскую прозу.
const (
	liveLimitBelowFloor         = "discovery_below_floor"
	liveLimitDiscoverySample    = "discovery_sample"
	liveLimitDiscoveryUndated   = "discovery_undated"
	liveLimitDiscoveryThin      = "discovery_thin"
	liveLimitDiscoveryFailed    = "discovery_failed"
	liveLimitPopulationSample   = "population_sample"
	LiveLimitAffiliationsBudget = "affiliations_budget"
	LiveLimitAffiliationsOff    = "affiliations_unavailable"
	LiveLimitLineageBudget      = "lineage_budget"
	LiveLimitLineageOff         = "lineage_unavailable"
	liveLimitScholarCapped      = "scholar_capped"
	// scholar_sampled — выборка из каждого месяца; при scholar_capped неизвестно, какие месяцы попали в сбор.
	liveLimitScholarSampled    = "scholar_sampled"
	liveLimitScholarJournals   = "scholar_journals"
	liveLimitScholarOff        = "scholar_unavailable"
	liveLimitArxivKept         = "arxiv_kept"
	liveLimitArxivSampled      = "arxiv_sampled"
	liveLimitArxivOff          = "arxiv_unavailable"
	liveLimitArxivNoTerms      = "arxiv_no_terms"
	liveLimitGitHubOff         = "github_unavailable"
	liveLimitHackerNewsOff     = "hn_unavailable"
	liveLimitChannelOff        = "channel_unavailable"
	liveLimitSourceOff         = "source_unavailable"
	liveLimitTranslationOff    = "translation_unavailable"
	liveLimitCollectorFallback = "collector_fallback"
	liveLimitAnalysisOff       = "analysis_unavailable"
	liveLimitCitationsOff      = "citations_unavailable"
	liveLimitCardOff           = "card_unavailable"
	liveLimitDiscoveryNoise    = "discovery_noise"
	liveLimitNoCandidates      = "no_candidates"
	liveLimitScholarNoTerms    = "scholar_no_terms"
)

// Ключи Values — контракт с переводами интерфейса: переименование сломает подстановку чисел.
const (
	liveValuePapers = "papers"
	liveValueFound  = "found"
)

type LiveLimit struct {
	Code   string         `json:"code,omitempty"`
	Values map[string]int `json:"values,omitempty"`
	Text   string         `json:"text"`
}

func liveLimitOf(code, text string, values map[string]int) LiveLimit {
	return LiveLimit{Code: code, Values: values, Text: text}
}

func liveLimitFail(code, source string, err error) LiveLimit {
	return LiveLimit{Code: code, Text: source + ": " + err.Error()}
}

func liveLimitTexts(texts []string) []LiveLimit {
	limits := make([]LiveLimit, 0, len(texts))
	for _, text := range texts {
		limits = append(limits, LiveLimit{Text: text})
	}
	return limits
}

// UnmarshalJSONFrom читает и голую строку: старые снимки хранят оговорки как []string.
func (l *LiveLimit) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() == '"' {
		var text string
		if err := json.UnmarshalDecode(dec, &text); err != nil {
			return fmt.Errorf("live limit: read text: %w", err)
		}
		*l = LiveLimit{Text: text}
		return nil
	}
	type plain LiveLimit
	var value plain
	if err := json.UnmarshalDecode(dec, &value); err != nil {
		return fmt.Errorf("live limit: read object: %w", err)
	}
	*l = LiveLimit(value)
	return nil
}
