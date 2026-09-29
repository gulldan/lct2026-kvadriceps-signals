package application

import (
	"fmt"
	"maps"
	"slices"
)

// Карты появления (KEM) и распространения (KIM) — Yoon (2012), Expert Systems with Applications 39(16),
// doi 10.1016/j.eswa.2012.04.059. Отличия: распространение — по авторским группам, порог — медиана набора.
const (
	liveEmergenceMethod = "yoon-2012-kem-kim-v1"
	// tw у Юна: последний месяц весит 1, месяц перед ним 0,95 и так далее.
	liveEmergenceTimeWeight = 0.05
	// Две точки — это одна разность: ни уровня, ни прироста.
	liveEmergenceMinPoints = 3

	liveEmergenceWeak       = "weak_signal"
	liveEmergenceStrong     = "strong_signal"
	liveEmergenceKnown      = "well_known"
	liveEmergenceLatent     = "latent"
	liveEmergenceUnmeasured = "unmeasured"
)

// LiveEmergence несёт медианы набора с собой: без них класс невоспроизводим.
type LiveEmergence struct {
	Method      string               `json:"method"`
	Class       string               `json:"class"`
	Basis       string               `json:"basis"`
	PeriodsUsed int                  `json:"periods_used"`
	DoVMean     *float64             `json:"dov_mean"`
	DoVGrowth   *float64             `json:"dov_growth"`
	DoDMean     *float64             `json:"dod_mean"`
	DoDGrowth   *float64             `json:"dod_growth"`
	Set         LiveEmergenceMedians `json:"set,omitzero"`
}

type LiveEmergenceMedians struct {
	Size      int     `json:"size"`
	DoVMean   float64 `json:"dov_mean"`
	DoVGrowth float64 `json:"dov_growth"`
	DoDMean   float64 `json:"dod_mean"`
	DoDGrowth float64 `json:"dod_growth"`
}

// У зонтика поиска карты нет: nil-безопасность избавляет каждое правило от отдельной ветки.
func (e *LiveEmergence) classified(class string) bool {
	return e != nil && e.Class == class
}

type liveEmergenceMeasure struct {
	periods              int
	dovMean, dodMean     float64
	dovGrowth, dodGrowth *float64
}

func (m liveEmergenceMeasure) measured() bool {
	return m.periods >= liveEmergenceMinPoints && m.dovGrowth != nil && m.dodGrowth != nil
}

// Текущий месяц обрезан по день наблюдения, и его доля занижена у всех тем.
func liveMeasureEmergence(periods []LivePeriod, series []StudyPeriod) liveEmergenceMeasure {
	kept := []int{}
	for i := range min(len(periods), len(series)) {
		if periods[i].Complete && series[i].Population > 0 {
			kept = append(kept, i)
		}
	}
	dov := make([]float64, 0, len(kept))
	dod := make([]float64, 0, len(kept))
	for k, i := range kept {
		weight := 1 - liveEmergenceTimeWeight*float64(len(kept)-1-k)
		population := float64(series[i].Population)
		dov = append(dov, float64(series[i].Count)/population*weight)
		dod = append(dod, float64(series[i].Groups)/population*weight)
	}
	out := liveEmergenceMeasure{periods: len(kept)}
	if len(kept) > 0 {
		out.dovMean, out.dodMean = liveMean(dov), liveMean(dod)
		out.dovGrowth, out.dodGrowth = liveGrowthRate(dov), liveGrowthRate(dod)
	}
	return out
}

// Месяц без наблюдений в знаменатель не идёт: деление на ноль — не прирост.
func liveGrowthRate(points []float64) *float64 {
	positive := 0
	for _, x := range points {
		if x > 0 {
			positive++
		}
	}
	if positive < liveEmergenceMinPoints {
		return nil
	}
	sum, pairs := 0.0, 0
	for j := 0; j+1 < len(points); j++ {
		if points[j] > 0 {
			sum += (points[j+1] - points[j]) / points[j]
			pairs++
		}
	}
	if pairs == 0 {
		return nil
	}
	return new(sum / float64(pairs))
}

func liveMean(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func liveMedian(xs []float64) float64 {
	sorted := slices.Sorted(slices.Values(xs))
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// Только измеренные темы: иначе «нет данных» тянуло бы порог к нулю.
func liveEmergenceMedians(periods []LivePeriod, set [][]StudyPeriod) LiveEmergenceMedians {
	var dovMeans, dovGrowths, dodMeans, dodGrowths []float64
	for _, series := range set {
		m := liveMeasureEmergence(periods, series)
		if !m.measured() {
			continue
		}
		dovMeans, dodMeans = append(dovMeans, m.dovMean), append(dodMeans, m.dodMean)
		dovGrowths, dodGrowths = append(dovGrowths, *m.dovGrowth), append(dodGrowths, *m.dodGrowth)
	}
	if len(dovMeans) == 0 {
		return LiveEmergenceMedians{}
	}
	return LiveEmergenceMedians{Size: len(dovMeans), DoVMean: liveMedian(dovMeans), DoVGrowth: liveMedian(dovGrowths),
		DoDMean: liveMedian(dodMeans), DoDGrowth: liveMedian(dodGrowths)}
}

// Пустое семейство — промах запроса, а не тема с нулевым уровнем.
func liveFamilyMedians(periods []LivePeriod, members [][]StudyPaper) LiveEmergenceMedians {
	set := make([][]StudyPeriod, 0, len(members))
	for _, papers := range members {
		if len(papers) > 0 {
			set = append(set, liveSeries(periods, papers))
		}
	}
	return liveEmergenceMedians(periods, set)
}

// Медианы по тому же набору, поэтому вызывается до присвоения стадий.
func liveEmergenceMap(periods []LivePeriod, set map[string][]StudyPeriod) map[string]*LiveEmergence {
	medians := liveEmergenceMedians(periods, slices.Collect(maps.Values(set)))
	out := make(map[string]*LiveEmergence, len(set))
	for id, series := range set {
		out[id] = liveEmergenceOf(periods, series, medians)
	}
	return out
}

// liveEmergenceOf: при расхождении карт тема латентна — у Юна слабый сигнал — пересечение карт.
func liveEmergenceOf(periods []LivePeriod, series []StudyPeriod, set LiveEmergenceMedians) *LiveEmergence {
	m := liveMeasureEmergence(periods, series)
	out := &LiveEmergence{Method: liveEmergenceMethod, Class: liveEmergenceUnmeasured, PeriodsUsed: m.periods,
		DoVGrowth: m.dovGrowth, DoDGrowth: m.dodGrowth, Set: set}
	if m.periods > 0 {
		out.DoVMean, out.DoDMean = new(m.dovMean), new(m.dodMean)
	}
	switch {
	case m.periods < liveEmergenceMinPoints:
		out.Basis = fmt.Sprintf("не измерено: полных месяцев с известным населением %d, нужно не меньше %d",
			m.periods, liveEmergenceMinPoints)
	case !m.measured():
		out.Basis = fmt.Sprintf("не измерено: месяцев с упоминаниями меньше %d, прирост посчитать не на чем",
			liveEmergenceMinPoints)
	case set.Size == 0:
		out.Basis = "не измерено: в наборе нет тем с измеренным приростом, медианы сравнивать не с чем"
	default:
		kem := liveQuadrant(m.dovMean, *m.dovGrowth, set.DoVMean, set.DoVGrowth)
		kim := liveQuadrant(m.dodMean, *m.dodGrowth, set.DoDMean, set.DoDGrowth)
		out.Class = liveEmergenceLatent
		if kem == kim {
			out.Class = kem
		}
		out.Basis = fmt.Sprintf("видимость %s медианы набора, прирост %s; распространение %s медианы, прирост %s; медианы по %d темам",
			liveAboveMedian(m.dovMean, set.DoVMean), liveAboveMedian(*m.dovGrowth, set.DoVGrowth),
			liveAboveMedian(m.dodMean, set.DoDMean), liveAboveMedian(*m.dodGrowth, set.DoDGrowth), set.Size)
	}
	return out
}

// «Высокий» — строго выше медианы.
func liveQuadrant(mean, growth, medianMean, medianGrowth float64) string {
	high, growing := mean > medianMean, growth > medianGrowth
	switch {
	case !high && growing:
		return liveEmergenceWeak
	case high && growing:
		return liveEmergenceStrong
	case high:
		return liveEmergenceKnown
	default:
		return liveEmergenceLatent
	}
}

func liveAboveMedian(value, median float64) string {
	if value > median {
		return "выше"
	}
	return "не выше"
}

func liveEmergenceRank(emergence *LiveEmergence) int {
	order := []string{liveEmergenceWeak, liveEmergenceStrong, liveEmergenceLatent, liveEmergenceKnown}
	if emergence == nil {
		return len(order)
	}
	if rank := slices.Index(order, emergence.Class); rank >= 0 {
		return rank
	}
	return len(order)
}
