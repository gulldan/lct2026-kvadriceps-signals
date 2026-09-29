package application

import (
	"math"
	"strings"
)

const liveBurstMethod = "coverage-adjusted-monthly-residual-v1"

const liveBurstTarget = "abrupt_complete_month_activity"

// LiveBurst — флаг отбора, отдельный от стадии; Score — остаток с поправкой на сверхдисперсию, а не вероятность.
type LiveBurst struct {
	Method          string   `json:"method"`
	Status          string   `json:"status"`
	Flag            *bool    `json:"flag"`
	Period          string   `json:"period,omitempty"`
	Baseline        []string `json:"baseline,omitempty"`
	Count           int      `json:"count"`
	Expected        *float64 `json:"expected"`
	Score           *float64 `json:"score"`
	ShareRatio      *float64 `json:"share_ratio"`
	RawGrowthFlag   *bool    `json:"raw_growth_flag"`
	PartialExcluded []string `json:"partial_excluded,omitempty"`
	QualityStatus   string   `json:"quality_status"`
	DetectionTarget string   `json:"detection_target"`
	Role            string   `json:"role"`
}

func liveBurst(topic LiveTopic, periods []LivePeriod, categories, previous string, limits []LiveLimit) LiveBurst {
	b := LiveBurst{Method: liveBurstMethod, Status: "insufficient_history", QualityStatus: liveQualityDevelopment,
		DetectionTarget: liveBurstTarget, Role: "descriptive_screening"}
	if categories == "" || (previous != "" && previous != categories) {
		b.Status = "coverage_changed"
		return b
	}
	for _, limit := range append(limits[:len(limits):len(limits)], topic.Limits...) {
		if limit.Code == liveLimitArxivOff || liveTemporalCoverageLimit(limit) ||
			strings.Contains(limit.Text, "arxiv") || strings.Contains(limit.Text, "enumerated") {
			b.Status = "incomplete_coverage"
			return b
		}
	}
	counts := map[string]int{}
	for _, row := range topic.Series {
		counts[row.Period] = row.Count
	}
	complete := []LivePeriod{}
	for _, period := range periods {
		if !period.Complete {
			b.PartialExcluded = append(b.PartialExcluded, period.ID)
			continue
		}
		complete = append(complete, period)
	}
	if len(complete) < 7 {
		return b
	}
	// Детектору нужна только база в шесть месяцев: отсутствие более ранней истории не портит текущее окно.
	complete = complete[len(complete)-7:]
	if status := liveBurstWindowStatus(complete, counts); status != "" {
		b.Status = status
		return b
	}
	baseline, target := complete[len(complete)-7:len(complete)-1], complete[len(complete)-1]
	population, observed := 0, 0
	for _, period := range baseline {
		population += period.Population
		observed += counts[period.ID]
		b.Baseline = append(b.Baseline, period.ID)
	}
	// Сглаживание Джеффриса держит пустую базу конечной; эмпирическая дисперсия не даёт завысить оценку изменчивого ряда.
	rate := (float64(observed) + .5) / (float64(population) + 1)
	dispersion := 0.0
	for _, period := range baseline {
		expected := float64(period.Population) * rate
		residual := float64(counts[period.ID]) - expected
		dispersion += residual * residual / max(expected*(1-rate), .5)
	}
	dispersion = max(1, dispersion/float64(len(baseline)-1))
	expected := float64(target.Population) * rate
	count := counts[target.ID]
	score := (float64(count) - expected) / math.Sqrt(max(expected*(1-rate)*dispersion, .5))
	ratio := (float64(count) / float64(target.Population)) / rate
	prior := counts[baseline[len(baseline)-1].ID]
	b.Status, b.Period, b.Count = "measured", target.ID, count
	b.Expected, b.Score, b.ShareRatio = new(expected), new(score), new(ratio)
	b.Flag = new(count >= 5 && ratio >= 2 && score >= 3)
	b.RawGrowthFlag = new(count >= 5 && float64(count) >= 2*float64(max(1, prior)))
	return b
}

func liveBurstWindowStatus(periods []LivePeriod, counts map[string]int) string {
	for _, period := range periods {
		if period.Population <= 0 {
			return "unknown_population"
		}
		count, ok := counts[period.ID]
		if !ok {
			return "insufficient_history"
		}
		if count < 0 || count > period.Population {
			return "inconsistent_counts"
		}
	}
	return ""
}
