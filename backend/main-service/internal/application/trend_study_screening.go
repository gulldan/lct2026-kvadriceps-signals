package application

import (
	_ "embed"
	"encoding/json/v2"
)

// Решения — документальный разбор аннотаций Codex, а не независимая экспертная разметка; хеши источников и
// причины хранятся отдельно от посчитанного сигнала.
//
//go:embed trend_study_screening.json
var studyScreeningJSON []byte

type studyDecision struct {
	PaperID     string `json:"paper_id"`
	ContentHash string `json:"content_hash"`
	Included    bool   `json:"included"`
	Reason      string `json:"reason"`
}

func studyDecisions() map[string]map[string]studyDecision {
	var review struct {
		Version        string `json:"version"`
		CorpusRevision string `json:"corpus_revision"`
		Families       []struct {
			ID        string          `json:"id"`
			Decisions []studyDecision `json:"decisions"`
		} `json:"families"`
	}
	corpus, err := studyCatalog()
	if err != nil || json.Unmarshal(studyScreeningJSON, &review) != nil ||
		review.Version != "acl-family-abstract-screening-v1" || review.CorpusRevision != corpus.Revision {
		return nil
	}
	result := map[string]map[string]studyDecision{}
	for _, family := range review.Families {
		if result[family.ID] != nil {
			return nil
		}
		result[family.ID] = map[string]studyDecision{}
		for _, decision := range family.Decisions {
			if decision.PaperID == "" || decision.ContentHash == "" || decision.Reason == "" ||
				result[family.ID][decision.PaperID].PaperID != "" {
				return nil
			}
			result[family.ID][decision.PaperID] = decision
		}
	}
	return result
}

func studyScreening(family studyFamily, papers []StudyPaper, decisions map[string]studyDecision) string {
	if len(decisions) == 0 {
		return studyScreeningNone
	}
	for _, paper := range papers {
		if family.pattern.MatchString(paper.Title) && decisions[paper.ID].ContentHash != paper.ContentHash {
			return studyScreeningNone
		}
	}
	return "abstracts"
}
