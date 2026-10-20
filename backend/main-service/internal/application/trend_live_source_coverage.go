package application

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// Версия политики отделена от обученных весов: прежний отчёт качества не измерял исключение неподходящего источника.
const liveSourceCoverageVersion = "+source-coverage-v5"

type liveJournalPaper struct {
	paper  StudyPaper
	source string
}

func liveJournalSource(paper StudyPaper, meta LivePaperMetadata) bool {
	if cmp.Or(liveJournalProvider(paper.ID), liveJournalProvider(meta.Category)) == "" {
		return false
	}
	// Копия того же препринта в агрегаторе не доказывает покрытие вне исходного канала.
	if passportHost(paper.URL) == liveCardArxivHost || passportHost(paper.URL) == "export.arxiv.org" ||
		strings.HasPrefix(liveDOI(paper, meta), "10.48550/arxiv.") {
		return false
	}
	return slices.Contains([]string{liveGenreArticle, liveGenreReview, "conference-paper", "data-paper",
		"dissertation", liveMLExtractPreprint}, meta.Genre)
}

// Одинаковое имя не позволяет перенести доказательство от проверенного кандидата к непроверенному.
func (s *TrendLiveService) weakSignalCandidateEvidence(ctx, sourceCtx context.Context, candidates []LiveClassifierCandidate,
	phrases []string, periods []LivePeriod, embeddingModel string,
) []*WeakSignalEvidence {
	unique := slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(phrases))), func(p string) bool { return p == "" })
	facts := s.weakSignalFacts(sourceCtx, unique, periods, embeddingModel)
	rows := make([]*WeakSignalEvidence, len(candidates))
	for i, candidate := range candidates {
		own := WeakSignalEvidence{}
		if shared := facts[phrases[i]]; shared != nil {
			own = *shared
			own.Journals = liveJournalCandidateEvidence(candidate, periods)
		}
		rows[i] = &own
	}
	s.rateWeakSignalEvidence(ctx, phrases, rows)
	return rows
}

// Совпадение фразы в статье не доказывает покрытие механизма: добор до независимой проверки
// не должен снимать штраф лишь потому, что в его поисковой выдаче есть научная публикация.
func liveJournalMechanismSource(candidate LiveTopic, sourceID string) bool {
	return liveVerifiedMechanism(candidate) && slices.ContainsFunc(candidate.Qualification.Evidence,
		func(e LiveQualificationEvidence) bool { return e.SourceID == sourceID })
}

func liveJournalCandidateEvidence(candidate LiveClassifierCandidate, periods []LivePeriod) *LiveRatingEvidence {
	evidence := &LiveRatingEvidence{}
	if len(periods) == 0 {
		return evidence
	}
	evidence.Window = periods[0].Start + " — " + periods[len(periods)-1].EndExclusive + " (end exclusive)"
	documents := slices.Clone(candidate.journals)
	slices.SortStableFunc(documents, func(a, b liveJournalPaper) int { return cmp.Compare(b.paper.Date, a.paper.Date) })
	seen := map[string]bool{}
	sources := []string{}
	for _, document := range documents {
		paper := document.paper
		// Относимость уже доказана независимой проверкой исходной сущности и механизма.
		// Повторное буквальное совпадение фразы теряет подтверждённые синонимы и промежуточные слова.
		if livePeriodIndex(periods, paper.Date) < 0 {
			continue
		}
		title := liveScholarTitleKey(paper.Title)
		canonical := cmp.Or(liveResearchDOI(paper.CanonicalID), strings.ToLower(cmp.Or(paper.CanonicalID, paper.ID)))
		if seen[title] || seen[canonical] {
			continue
		}
		seen[title], seen[canonical] = true, true
		evidence.Total++
		source := cmp.Or(document.source, liveJournalProvider(paper.ID))
		if source != "" && !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
		if len(evidence.Titles) < 5 {
			evidence.Titles = append(evidence.Titles, LiveRatingTitle(paper.Title, paper.Date))
		}
	}
	slices.Sort(sources)
	evidence.Source = strings.Join(sources, ", ")
	return evidence
}

func (e *WeakSignalEvidence) emptyArxivWindow() bool {
	return e != nil && e.Window != nil && e.Window.Total == 0 && e.Window.Documents == 0
}

func (e *WeakSignalEvidence) journalCoverage() bool {
	return e.emptyArxivWindow() && e.hasJournalEvidence()
}

// Среднее даёт ровно нулевой вклад, в отличие от медианы для сбоев. Числа из другой библиотеки нельзя
// подставлять в веса, обученные на объёме и темпе препринтов: у источников разные масштабы.
func (m WeakSignalModel) applySourceCoverage(inputs []float64, ordered []string,
	evidence map[string]*WeakSignalEvidence,
) {
	width := len(weakSignalKinds()[0])
	for side, phrase := range []string{ordered[0], ordered[len(ordered)-1]} {
		if evidence[phrase].journalCoverage() {
			start := side * width
			copy(inputs[start:start+width], m.Mean[start:start+width])
		}
	}
}

func (e *WeakSignalEvidence) ratingEvidence() LiveRatingEvidence {
	if e.hasJournalEvidence() && (e.Window == nil || e.journalCoverage()) {
		return *e.Journals
	}
	return LiveRatingEvidence{Total: e.Window.Total, Titles: e.Window.Titles}
}

func (e *WeakSignalEvidence) hasJournalEvidence() bool {
	return e != nil && e.Journals != nil && e.Journals.Total > 0
}

func (e *WeakSignalEvidence) coverageDescription() string {
	other := "пустой канал не доказывает отсутствие исследований"
	if e.hasJournalEvidence() {
		other = fmt.Sprintf("в научных источниках вне arXiv найдено не менее %d работ по проверенному механизму", e.Journals.Total)
	}
	return "в arXiv работ нет; " + other + "; пустое окно arXiv исключено из оценки, вклад равен нулю"
}

func liveJournalProvider(id string) string {
	provider, _, _ := strings.Cut(strings.TrimPrefix(id, liveRecordPrefix), ":")
	return map[string]string{livePubMedSource: "PubMed", liveScholarSource: "OpenAlex",
		"datacite": liveDataCiteName, "openaire": liveOpenAIREName, "crossref": "Crossref",
		"europe pmc": "Europe PMC", "europepmc": "Europe PMC", "biorxiv": "bioRxiv",
		"medrxiv": "medRxiv", "chemrxiv": "ChemRxiv"}[strings.ToLower(strings.TrimSpace(provider))]
}
