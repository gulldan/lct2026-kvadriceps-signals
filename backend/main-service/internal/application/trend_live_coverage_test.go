package application

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSearchWindowStratifiesCappedMatches(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	// Окно переполняет потолок в пять тысяч: проверяется поведение на переполнении.
	window := make([]StudyPaper, 0, len(periods)*600)
	for month, period := range periods {
		for i := range 600 {
			window = append(window, StudyPaper{ID: fmt.Sprintf("%02d-%03d", month, i),
				Title: "Graph learning", Date: period.Start})
		}
	}
	s := NewTrendLiveService(nil, nil)
	s.setWindow(window, nil)
	plan := liveFixtureQueryPlan("graph", []string{"graph"})
	request := LiveWindowSearch{Query: "graph", Periods: periods, Plan: &plan}
	first, err := s.SearchWindow(t.Context(), request)
	// Выборка распределена по месяцам и стадия по ней измерима, поэтому это выборка, а не пропуск покрытия.
	sampled := slices.ContainsFunc(first.Limits, func(limit LiveLimit) bool {
		return limit.Code == liveLimitArxivSampled
	})
	if err != nil || first.Total != len(window) || len(first.Papers) != liveWindowSearchCap ||
		!sampled || liveTemporalCoverageLimited(first.Limits) {
		t.Fatalf("search cap or total changed: %+v, %v", first.Limits, err)
	}
	counts, ids := map[string]int{}, make([]string, 0, len(first.Papers))
	for _, paper := range first.Papers {
		counts[paper.Date]++
		ids = append(ids, paper.ID)
	}
	for _, period := range periods {
		count := counts[period.Start]
		if count < liveWindowSearchCap/len(periods) || count > liveWindowSearchCap/len(periods)+1 {
			t.Fatalf("month %s was starved by input order: %d", period.ID, count)
		}
	}
	reversed := slices.Clone(window)
	slices.Reverse(reversed)
	s.setWindow(reversed, nil)
	second, err := s.SearchWindow(t.Context(), request)
	if err != nil || second.Total != first.Total || len(second.Papers) != len(ids) {
		t.Fatalf("reverse window changed the result size: %d, %v", second.Total, err)
	}
	for i, paper := range second.Papers {
		if paper.ID != ids[i] {
			t.Fatalf("reverse window changed selected source %d: %s != %s", i, paper.ID, ids[i])
		}
	}
}

func TestCappedCandidateCoverageSurvivesQualification(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 10000
	}
	papers := make([]StudyPaper, 0, 18)
	for month := range 6 {
		for i := range 3 {
			id := fmt.Sprintf("source-%d-%d", month, i)
			papers = append(papers, StudyPaper{ID: id, Title: "Phase coherence for synthetic speech",
				Date: periods[month].Start, Authors: []string{id}})
		}
	}
	candidate := liveCandidate{gram: "phase coherence", papers: papers}
	bar := liveAbsoluteBar()
	plain := liveCandidateTopic(LiveTopic{}, candidate, periods, bar, nil)
	weak := liveEvidenceTiers()[len(liveEvidenceTiers())-1]
	if code, _ := liveRejectReason(plain, weak, bar); code != liveRejectHistory || plain.Growth.DeltaPP == nil {
		t.Fatalf("complete coverage lost the existing history rule: %s, %+v", code, plain.Growth)
	}
	if len(liveDiscover(papers, nil, periods, nil, 2, 1)) != 0 {
		t.Fatal("complete coverage lost the recent evidence gate")
	}
	for _, code := range []string{liveLimitArxivKept, liveLimitScholarCapped} {
		t.Run(code, func(t *testing.T) {
			umbrella := LiveTopic{Limits: []LiveLimit{{Code: code, Text: "собрана только часть временного корпуса"}}}
			found, _ := liveSearchCandidates(&umbrella, papers, nil, periods)
			if len(found) == 0 {
				t.Fatal("capped observations were rejected by an unmeasured recency gate")
			}
			topic := liveCandidateTopic(umbrella, candidate, periods, bar, nil)
			if refused, _ := liveRejectReason(topic, weak, bar); refused != "" {
				t.Fatalf("capped observations were rejected by %s", refused)
			}
			assertPartial := func(got LiveTopic) {
				t.Helper()
				burst := liveBurst(got, periods, "cs", "cs", nil)
				if burst.Status != "incomplete_coverage" || burst.Score != nil || burst.Flag != nil {
					t.Fatalf("усечённый корпус стал измеренным всплеском: %+v", burst)
				}
				if !liveTemporalCoverageLimited(got.Limits) || got.Stage != studyStageUnassessed ||
					got.Momentum != studyUnknown || got.Signals.Novelty != studyUnknown || got.GrowthStart != "" ||
					got.Growth.DeltaPP != nil || got.Growth.ShareRatio != nil || got.Growth.CountRatio != nil ||
					got.Growth.Prior.Share != nil || got.Growth.Recent.Share != nil || liveScienceAge(got).long {
					t.Fatalf("partial coverage became a temporal measurement: %+v", got.StudyTopic)
				}
				count := 0
				for _, row := range got.Series {
					count += row.Count
					if row.Share != nil {
						t.Fatal("sample numerator was divided by complete population")
					}
				}
				if count != got.DocumentsCount || got.Confidence.Total != 8 ||
					!strings.Contains(strings.Join(got.Confidence.Unmeasured, " "), "сбор усечён") {
					t.Fatalf("observed counts or explicit uncertainty were lost: %+v", got.Confidence)
				}
			}
			assertPartial(topic)
			byID := map[string]StudyPaper{}
			qualification := LiveQualification{Label: "Фазовое обнаружение синтетической речи"}
			for _, paper := range []StudyPaper{papers[0], papers[3], papers[6]} {
				byID[paper.ID] = paper
				qualification.Evidence = append(qualification.Evidence,
					LiveQualificationEvidence{SourceID: paper.ID, Quote: paper.Title})
			}
			qualified := liveQualifiedTopic(topic, qualification, byID, periods, bar)
			assertPartial(qualified)
			if qualified.EvidenceLevel != liveEvidenceThin {
				t.Fatal("source qualification promoted unmeasured growth")
			}
		})
	}
}
