package application

import (
	"cmp"
	"slices"
	"time"
)

// LivePeriod — календарный месяц; текущий обрезан по день наблюдения.
type LivePeriod struct {
	ID           string `json:"period"`
	Start        string `json:"start"`
	EndExclusive string `json:"end_exclusive"`
	Complete     bool   `json:"complete"`
	Population   int    `json:"population"`
}

func liveDay(date string) time.Time {
	day, _ := time.Parse("2006-01-02", date)
	return day
}

func livePeriods(now time.Time) []LivePeriod {
	now = now.UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	start := month.AddDate(0, -(liveMonths - 1), 0)
	periods := make([]LivePeriod, 0, liveMonths)
	for i := range liveMonths {
		from := start.AddDate(0, i, 0)
		to := from.AddDate(0, 1, 0)
		complete := !to.After(now)
		if to.After(cutoff) {
			to = cutoff
		}
		periods = append(periods, LivePeriod{
			ID: from.Format("2006-01"), Start: from.Format(time.DateOnly),
			EndExclusive: to.Format(time.DateOnly), Complete: complete,
		})
	}
	return periods
}

func livePeriodIndex(periods []LivePeriod, date string) int {
	for i, period := range periods {
		if date >= period.Start && date < period.EndExclusive {
			return i
		}
	}
	return -1
}

func liveSeries(periods []LivePeriod, papers []StudyPaper) []StudyPeriod {
	groups := studyGroups(papers)
	firstIndex := make([]int, len(groups))
	for i, group := range groups {
		firstIndex[i] = len(periods)
		for _, paper := range group {
			if idx := livePeriodIndex(periods, paper.Date); idx >= 0 {
				firstIndex[i] = min(firstIndex[i], idx)
			}
		}
	}
	series := make([]StudyPeriod, 0, len(periods))
	for idx, period := range periods {
		row := StudyPeriod{Period: period.ID, Population: period.Population}
		inPeriod := func(paper StudyPaper) bool { return livePeriodIndex(periods, paper.Date) == idx }
		row.Count = studyCount(papers, inPeriod)
		for i, group := range groups {
			if !slices.ContainsFunc(group, inPeriod) {
				continue
			}
			row.Groups++
			if firstIndex[i] == idx {
				row.NewGroups++
			} else {
				row.RetainedGroups++
			}
		}
		if period.Population > 0 {
			row.Share = new(float64(row.Count) / float64(period.Population))
		}
		series = append(series, row)
	}
	return series
}

// Стадия — по полным месяцам с нормировкой на население окна; liveCompare включает и месяц наблюдения.
func liveAssess(topic *LiveTopic, periods []LivePeriod, papers []StudyPaper) {
	complete := []StudyPeriod{}
	for i, period := range periods {
		if period.Complete {
			complete = append(complete, topic.Series[i])
		}
	}
	topic.FirstYear, topic.ActiveYears = studyActivity(topic.Series)
	topic.Groups, topic.TopGroupShare = studyConcentration(papers, len(papers) > 0)
	if len(complete) >= 12 {
		n := len(complete)
		topic.Growth = studyGrowth(liveAggregate(complete[n-12:n-6]), liveAggregate(complete[n-6:]), true)
	}
	topic.Momentum = studyMomentum(topic.Growth)
	topic.GrowthStart = studyGrowthStart(complete)
	slices.SortFunc(topic.External, func(a, b StudyEvent) int { return cmp.Compare(a.Date, b.Date) })
	if len(topic.External) > 0 {
		topic.EarliestExternal = topic.External[0].Date
	}
	firstIndex := liveTakeoff(topic.Series, len(papers))
	signals := StudySignals{Novelty: studyUnknown, Momentum: topic.Momentum, Coherence: liveCoherenceQuery,
		Spread: studyUnknown, Application: "none_observed", Concentration: studyUnknown}
	switch {
	case firstIndex < 0:
	case firstIndex == 0:
		signals.Novelty = studyNoveltyEarlier
	case firstIndex >= len(topic.Series)-6:
		signals.Novelty = studyNoveltyNew
	default:
		signals.Novelty = "recent"
	}
	if len(complete) > 0 {
		signals.Spread = studySpreadLevel(complete[len(complete)-1].NewGroups)
	}
	signals.Concentration = studyConcentrationLevel(topic.TopGroupShare)
	if slices.ContainsFunc(topic.External, func(e StudyEvent) bool { return e.Kind == studyKindImplementation }) {
		signals.Application = "implementations"
	}
	topic.Signals = signals
	// Проверка «тема существовала до окна» стоит выше проверки узкого корпуса, иначе давнее направление с
	// малым числом групп получает early_signal.
	switch {
	case len(papers) == 0:
		topic.Stage = studyStageUnassessed
	case signals.Novelty == studyNoveltyEarlier:
		topic.Stage = "established"
	case liveEarlySignal(topic, firstIndex):
		topic.Stage = liveStageEarly
	case topic.ActiveYears >= 2 && topic.Momentum == "up":
		topic.Stage = "emerging_signal"
	default:
		topic.Stage = studyStageUnassessed
	}
	liveApplyTemporalCoverage(topic)
}

func liveAggregate(rows []StudyPeriod) StudyPeriod {
	merged := StudyPeriod{Period: rows[0].Period + "–" + rows[len(rows)-1].Period}
	for _, row := range rows {
		merged.Count += row.Count
		merged.Population += row.Population
		merged.Groups += row.Groups
		merged.NewGroups += row.NewGroups
		merged.RetainedGroups += row.RetainedGroups
	}
	if merged.Population > 0 {
		merged.Share = new(float64(merged.Count) / float64(merged.Population))
	}
	return merged
}

// Порог max(3, 5 %) работ окна: случайное раннее совпадение не делает направление «раньше».
func liveTakeoff(series []StudyPeriod, total int) int {
	threshold := max(3, total/20)
	if idx := slices.IndexFunc(series, func(row StudyPeriod) bool { return row.Count >= threshold }); idx >= 0 {
		return idx
	}
	return slices.IndexFunc(series, func(row StudyPeriod) bool { return row.Count > 0 })
}

// LiveComparison — соседние окна равной длины по день наблюдения; свежее начинается за шесть месяцев до исключающего конца.
type LiveComparison struct {
	PriorStart   string `json:"prior_start"`
	RecentStart  string `json:"recent_start"`
	EndExclusive string `json:"end_exclusive"`
	Days         int    `json:"days"`
	Prior        int    `json:"prior"`
	Current      int    `json:"current"`
	Fresh        int    `json:"fresh"`
}

func liveCompare(periods []LivePeriod, papers []StudyPaper) *LiveComparison {
	if len(periods) == 0 {
		return nil
	}
	end := liveDay(periods[len(periods)-1].EndExclusive)
	// Вычитание месяцев прижимается к концу месяца (31 августа → 28/29 февраля), а не уходит в март, как у AddDate.
	month := time.Date(end.Year(), end.Month()-6, 1, 0, 0, 0, 0, time.UTC)
	lastDay := month.AddDate(0, 1, -1).Day()
	start := month.AddDate(0, 0, min(end.Day(), lastDay)-1)
	prior := start.Add(-end.Sub(start))
	if prior.Before(liveDay(periods[0].Start)) {
		return nil
	}
	c := &LiveComparison{
		PriorStart: prior.Format(time.DateOnly), RecentStart: start.Format(time.DateOnly),
		EndExclusive: end.Format(time.DateOnly), Days: int(end.Sub(start) / (24 * time.Hour)),
	}
	for _, p := range papers {
		if p.Date >= c.RecentStart && p.Date < c.EndExclusive {
			c.Current++
		}
		if p.Date >= c.PriorStart && p.Date < c.RecentStart {
			c.Prior++
		}
	}
	for _, group := range studyGroups(papers) {
		first := c.EndExclusive
		for _, p := range group {
			if p.Date < first {
				first = p.Date
			}
		}
		if first >= c.RecentStart && first < c.EndExclusive {
			c.Fresh++
		}
	}
	return c
}

// Ранняя стадия по опубликованным критериям: постоянство и сообщество — Carley и др. (2018, Scientometrics
// 115(1)), новизна и рост — Rotolo и др. (2015, Research Policy 44(10)); доля группы ≤ 0,5 — наша калибровка.
const (
	liveEarlyMinPeriods  = 3
	liveEarlyMinRecords  = 7
	liveEarlyBaseline    = 3
	liveEarlyMaxTopShare = 0.5
)

func liveEarlySignal(topic *LiveTopic, firstIndex int) bool {
	// Новизна: первая запись после базового начала окна либо слабый сигнал на обеих картах Юна.
	if firstIndex < liveEarlyBaseline && !topic.Emergence.classified(liveEmergenceWeak) {
		return false
	}
	active, records := 0, len(topic.External)
	for _, period := range topic.Series {
		if period.Count > 0 {
			active++
		}
		records += period.Count
	}
	if active < liveEarlyMinPeriods || records < liveEarlyMinRecords {
		return false
	}
	if topic.Groups < 2 || topic.TopGroupShare != nil && *topic.TopGroupShare > liveEarlyMaxTopShare {
		return false
	}
	// Падающий признак в литературе относят к шуму, а не к слабым сигналам, поэтому спад снимает раннюю стадию.
	return topic.Momentum != "down"
}
