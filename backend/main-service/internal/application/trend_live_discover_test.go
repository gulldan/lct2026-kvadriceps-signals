package application

import (
	"fmt"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLiveDiscoverSkipsCandidatesBelowAdmissionMinimum(t *testing.T) {
	t.Parallel()
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	papers := make([]StudyPaper, 0, 46)
	for i := range 2 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("thin-%d", i),
			Title: "spectral holography sensing", Date: periods[len(periods)-1].Start,
			Authors: []string{fmt.Sprintf("Thin %d", i)}})
	}
	for i := range 4 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("solid-%d", i),
			Title: "photonic lattice interferometer", Date: periods[len(periods)-1-i%3].Start,
			Authors: []string{fmt.Sprintf("Solid %d", i)}})
	}
	for i := range 40 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("bg-%d", i),
			Title: "unrelated survey of methods", Date: periods[i%len(periods)].Start,
			Authors: []string{fmt.Sprintf("Background %d", i)}})
	}
	found := liveDiscoverWithCoverage(papers, nil, periods, nil, 2, 1, false)
	for _, candidate := range found {
		if len(candidate.papers) < liveCandidateMinDocuments {
			t.Fatalf("кандидат «%s» с %d документами не доживёт до допуска в %d",
				candidate.gram, len(candidate.papers), liveCandidateMinDocuments)
		}
	}
}

func TestLiveCandidateFamilyKeepsValidUTF8(t *testing.T) {
	family := liveCandidateFamily(liveCandidate{gram: "éclair détection", surface: "éclair détection"})
	if !utf8.ValidString(family.name) || family.name != "Éclair Détection" {
		t.Fatalf("имя кандидата испорчено: %q", family.name)
	}
}
