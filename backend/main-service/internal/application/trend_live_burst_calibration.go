package application

import (
	"math"
	"slices"
)

// LiveBurstCalibration — условный расчёт по сценариям: реальной точности он не даёт, распространённость
// и генератор — не эмпирические метки.
type LiveBurstCalibration struct {
	Method          string                       `json:"method"`
	DetectionTarget string                       `json:"detection_target"`
	Generator       string                       `json:"conditional_generator"`
	Interpretation  string                       `json:"interpretation"`
	Windows         []LiveBurstCalibrationWindow `json:"windows"`
	Scenarios       []LiveBurstScenario          `json:"scenarios"`
}

type LiveBurstCalibrationWindow struct {
	TopicID       string              `json:"topic_id"`
	Period        string              `json:"period"`
	Population    int                 `json:"target_population"`
	BaselineRate  float64             `json:"pre_target_share"`
	BurstMinimum  int                 `json:"burst_minimum_count"`
	GrowthMinimum int                 `json:"growth_minimum_count"`
	Scenarios     []LiveBurstScenario `json:"scenarios"`
}

type LiveBurstScenario struct {
	Multiplier float64 `json:"share_multiplier"`
	Windows    int     `json:"windows"`
	Burst      float64 `json:"burst_alert_probability"`
	Growth     float64 `json:"simple_growth_alert_probability"`
}

// CalibrateLiveBurst считает вероятности точно по биномиальной модели, без будущих счётчиков и подобранного порога.
func CalibrateLiveBurst(snapshot LiveSnapshot) (LiveBurstCalibration, error) {
	result := LiveBurstCalibration{
		Method: "conditional-binomial-burst-calibration-v1", DetectionTarget: liveBurstTarget,
		Generator: "Binomial(target population, multiplier * smoothed pre-target share); " +
			"fixed observed six-month baseline and exposure",
		Interpretation: "Conditional false alert probability at 1x; sensitivity to abrupt share increases at 2x/4x. " +
			"Synthetic generator assumptions, not independent documentary acceptance or real-world precision.",
		Windows:   []LiveBurstCalibrationWindow{},
		Scenarios: []LiveBurstScenario{{Multiplier: 1}, {Multiplier: 2}, {Multiplier: 4}},
	}
	evaluation, err := EvaluateLiveBurst(snapshot)
	if err != nil {
		return result, err
	}
	topics := slices.Concat(snapshot.Topics, snapshot.Discovered)
	for _, trial := range evaluation.Rows {
		if trial.Signal.Status != "measured" {
			continue
		}
		window := calibrateLiveBurstWindow(snapshot, topics, trial)
		for i := range result.Scenarios {
			aggregate := &result.Scenarios[i]
			probability := min(1, window.BaselineRate*aggregate.Multiplier)
			scenario := LiveBurstScenario{Multiplier: aggregate.Multiplier, Windows: 1,
				Burst:  liveBinomialTail(window.Population, window.BurstMinimum, probability),
				Growth: liveBinomialTail(window.Population, window.GrowthMinimum, probability)}
			window.Scenarios = append(window.Scenarios, scenario)
			aggregate.Windows++
			aggregate.Burst += scenario.Burst
			aggregate.Growth += scenario.Growth
		}
		result.Windows = append(result.Windows, window)
	}
	for i := range result.Scenarios {
		if result.Scenarios[i].Windows > 0 {
			result.Scenarios[i].Burst /= float64(result.Scenarios[i].Windows)
			result.Scenarios[i].Growth /= float64(result.Scenarios[i].Windows)
		}
	}
	return result, nil
}

func calibrateLiveBurstWindow(
	snapshot LiveSnapshot, topics []LiveTopic, trial LiveBurstTrial,
) LiveBurstCalibrationWindow {
	end := slices.IndexFunc(snapshot.Periods, func(p LivePeriod) bool { return p.ID == trial.Period })
	periods := snapshot.Periods[:end+1]
	period := periods[end]
	topic := topics[slices.IndexFunc(topics, func(t LiveTopic) bool { return t.ID == trial.TopicID })]
	topic.Series = slices.Clone(topic.Series)
	target := slices.IndexFunc(topic.Series, func(p StudyPeriod) bool { return p.Period == trial.Period })
	minimum := func(burst bool) int {
		low, high := 0, period.Population+1
		for low < high {
			middle := low + (high-low)/2
			topic.Series[target].Count = middle
			signal := liveBurst(topic, periods, snapshot.Categories, snapshot.Categories, snapshot.Limits)
			flag := signal.RawGrowthFlag
			if burst {
				flag = signal.Flag
			}
			if flag != nil && *flag {
				high = middle
			} else {
				low = middle + 1
			}
		}
		return low
	}
	return LiveBurstCalibrationWindow{
		TopicID: topic.ID, Period: period.ID, Population: period.Population,
		BaselineRate: *trial.Signal.Expected / float64(period.Population),
		BurstMinimum: minimum(true), GrowthMinimum: minimum(false),
	}
}

// Счёт от моды не даёт (1-p)^n уйти в ноль; рекуррентная сумма детерминирована, без случайного зерна.
func liveBinomialTail(n, threshold int, p float64) float64 {
	if threshold <= 0 {
		return 1
	}
	if threshold > n || p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	mode := int(math.Floor(float64(n+1) * p))
	lgN, _ := math.Lgamma(float64(n + 1))
	lgMode, _ := math.Lgamma(float64(mode + 1))
	lgRest, _ := math.Lgamma(float64(n - mode + 1))
	mass := math.Exp(lgN - lgMode - lgRest + float64(mode)*math.Log(p) + float64(n-mode)*math.Log1p(-p))
	tail, total := 0.0, mass
	if mode >= threshold {
		tail = mass
	}
	value := mass
	for k := mode; k > 0; k-- {
		value *= float64(k) / float64(n-k+1) * (1 - p) / p
		total += value
		if k-1 >= threshold {
			tail += value
		}
	}
	value = mass
	for k := mode; k < n; k++ {
		value *= float64(n-k) / float64(k+1) * p / (1 - p)
		total += value
		if k+1 >= threshold {
			tail += value
		}
	}
	return min(1, max(0, tail/total))
}
