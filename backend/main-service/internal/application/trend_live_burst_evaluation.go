package application

import (
	"cmp"
	"errors"
	"slices"
	"time"
)

const liveBurstEvaluationMethod = "dated-two-month-followup-v1"

// LiveBurstEvaluation сравнивает флаги с позже наблюдённой активностью фиксированных тем, а не с появлением во всём мире.
type LiveBurstEvaluation struct {
	Method       string               `json:"method"`
	ObservedAt   string               `json:"snapshot_observed_at"`
	SourceScope  string               `json:"source_scope"`
	OutcomeRule  string               `json:"outcome_rule"`
	OutcomeRole  string               `json:"outcome_role"`
	Quality      string               `json:"quality_status"`
	Rows         []LiveBurstTrial     `json:"rows"`
	Burst        LiveBurstConfusion   `json:"burst"`
	SimpleGrowth LiveBurstConfusion   `json:"simple_growth"`
	Incremental  LiveBurstIncremental `json:"incremental"`
}

type LiveBurstTrial struct {
	TopicID        string    `json:"topic_id"`
	Period         string    `json:"period"`
	Status         string    `json:"status"`
	Signal         LiveBurst `json:"signal"`
	FuturePeriods  []string  `json:"future_periods"`
	FutureCount    int       `json:"future_count"`
	FutureExposure int       `json:"future_exposure"`
	FutureRatio    *float64  `json:"future_to_baseline_share_ratio"`
	Sustained      *bool     `json:"sustained_activity"`
}

type LiveBurstConfusion struct {
	Evaluated int      `json:"evaluated"`
	TP        int      `json:"true_positive"`
	FP        int      `json:"false_positive"`
	FN        int      `json:"false_negative"`
	TN        int      `json:"true_negative"`
	Precision *float64 `json:"precision"`
	Recall    *float64 `json:"recall"`
}

type LiveBurstIncremental struct {
	AdditionalTrue  int `json:"additional_true_positive"`
	AdditionalFalse int `json:"additional_false_positive"`
	MissedTrue      int `json:"true_positives_only_simple_growth"`
	AvoidedFalse    int `json:"false_positives_only_simple_growth"`
}

// EvaluateLiveBurst не передаёт детектору будущего: исход — ровно два следующих полных месяца.
func EvaluateLiveBurst(snapshot LiveSnapshot) (LiveBurstEvaluation, error) {
	report := LiveBurstEvaluation{Method: liveBurstEvaluationMethod, ObservedAt: snapshot.ObservedAt,
		SourceScope: "fixed topic definitions in the saved observed corpus; retrospective conditional forecast",
		OutcomeRule: "At least five works across the next two complete months, a work in each month, " +
			"and their combined population-adjusted rate at least twice the smoothed six-month pre-target baseline rate.",
		OutcomeRole: "sustained future activity is a separate predictive-usefulness proxy, " +
			"not the detector's abrupt-activity target",
		Quality: liveQualityDevelopment, Rows: []LiveBurstTrial{}}
	if len(snapshot.Periods) < 7 || len(snapshot.Periods) > 120 || snapshot.Categories == "" ||
		len(snapshot.Topics)+len(snapshot.Discovered) > 128 {
		return report, errors.New("burst evaluation requires an identified corpus and 7..120 calendar months")
	}
	if err := validateLiveBurstPeriods(snapshot.Periods); err != nil {
		return report, err
	}
	topics := slices.Concat(snapshot.Topics, snapshot.Discovered)
	slices.SortFunc(topics, func(a, b LiveTopic) int { return cmp.Compare(a.ID, b.ID) })
	for index, topic := range topics {
		if topic.ID == "" || (index > 0 && topics[index-1].ID == topic.ID) {
			return report, errors.New("burst evaluation requires distinct named topic IDs")
		}
		trials, err := evaluateLiveBurstTopic(snapshot, topic)
		if err != nil {
			return report, err
		}
		for _, trial := range trials {
			report.addTrial(trial)
		}
	}
	for _, metric := range []*LiveBurstConfusion{&report.Burst, &report.SimpleGrowth} {
		if metric.TP+metric.FP > 0 {
			metric.Precision = new(float64(metric.TP) / float64(metric.TP+metric.FP))
		}
		if metric.TP+metric.FN > 0 {
			metric.Recall = new(float64(metric.TP) / float64(metric.TP+metric.FN))
		}
	}
	return report, nil
}

func validateLiveBurstPeriods(periods []LivePeriod) error {
	for i, period := range periods {
		start, err := time.Parse(time.DateOnly, period.Start)
		end, endErr := time.Parse(time.DateOnly, period.EndExclusive)
		if err != nil || endErr != nil || start.Day() != 1 || period.ID != start.Format("2006-01") || !end.After(start) ||
			(period.Complete && !end.Equal(start.AddDate(0, 1, 0))) || (i > 0 && periods[i-1].EndExclusive != period.Start) {
			return errors.New("burst evaluation months must be ordered, contiguous and unambiguous")
		}
	}
	return nil
}

func evaluateLiveBurstTopic(snapshot LiveSnapshot, topic LiveTopic) ([]LiveBurstTrial, error) {
	counts := map[string]int{}
	for _, row := range topic.Series {
		if _, exists := counts[row.Period]; exists {
			return nil, errors.New("burst evaluation rejects duplicate topic periods")
		}
		counts[row.Period] = row.Count
	}
	var trials []LiveBurstTrial
	for index := 6; index < len(snapshot.Periods); index++ {
		if !snapshot.Periods[index].Complete {
			continue
		}
		trials = append(trials, evaluateLiveBurstTrial(snapshot, topic, counts, index))
	}
	return trials, nil
}

func evaluateLiveBurstTrial(snapshot LiveSnapshot, topic LiveTopic, counts map[string]int, index int) LiveBurstTrial {
	period := snapshot.Periods[index]
	trial := LiveBurstTrial{TopicID: topic.ID, Period: period.ID, Status: "unknown_signal", FuturePeriods: []string{}}
	prefix := topic
	prefix.Series = nil
	for _, row := range topic.Series {
		if row.Period <= period.ID {
			prefix.Series = append(prefix.Series, row)
		}
	}
	trial.Signal = liveBurst(prefix, snapshot.Periods[:index+1], snapshot.Categories, snapshot.Categories, snapshot.Limits)
	if topic.Analysis != nil && topic.Analysis.Discovery != nil && topic.Analysis.Discovery.Sampled {
		trial.Signal.Status, trial.Signal.Flag, trial.Signal.RawGrowthFlag = "sampled_membership", nil, nil
	}
	if trial.Signal.Flag == nil || trial.Signal.RawGrowthFlag == nil {
		return trial
	}
	trial.Status = "right_censored"
	if index+2 < len(snapshot.Periods) {
		trial.observeFuture(period, snapshot.Periods[index+1:index+3], counts)
	}
	return trial
}

func (trial *LiveBurstTrial) observeFuture(period LivePeriod, futurePeriods []LivePeriod, counts map[string]int) {
	complete, eachActive := true, true
	for _, future := range futurePeriods {
		count, present := counts[future.ID]
		complete = complete && future.Complete && future.Population > 0 && present && count >= 0 && count <= future.Population
		eachActive = eachActive && count > 0
		trial.FuturePeriods = append(trial.FuturePeriods, future.ID)
		trial.FutureCount += count
		trial.FutureExposure += future.Population
	}
	if !complete {
		return
	}
	baseRate := *trial.Signal.Expected / float64(period.Population)
	ratio := (float64(trial.FutureCount) / float64(trial.FutureExposure)) / baseRate
	trial.Status, trial.FutureRatio = "evaluated", new(ratio)
	trial.Sustained = new(trial.FutureCount >= 5 && eachActive && ratio >= 2)
}

func (report *LiveBurstEvaluation) addTrial(trial LiveBurstTrial) {
	report.Rows = append(report.Rows, trial)
	if trial.Sustained == nil {
		return
	}
	signal, growth, sustained := *trial.Signal.Flag, *trial.Signal.RawGrowthFlag, *trial.Sustained
	liveBurstCount(&report.Burst, signal, sustained)
	liveBurstCount(&report.SimpleGrowth, growth, sustained)
	switch {
	case signal && !growth && sustained:
		report.Incremental.AdditionalTrue++
	case signal && !growth:
		report.Incremental.AdditionalFalse++
	case !signal && growth && sustained:
		report.Incremental.MissedTrue++
	case !signal && growth:
		report.Incremental.AvoidedFalse++
	}
}

func liveBurstCount(metric *LiveBurstConfusion, signal, outcome bool) {
	metric.Evaluated++
	switch {
	case signal && outcome:
		metric.TP++
	case signal:
		metric.FP++
	case outcome:
		metric.FN++
	default:
		metric.TN++
	}
}
