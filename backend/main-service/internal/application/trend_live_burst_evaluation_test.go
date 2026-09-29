package application

import (
	"encoding/json/v2"
	"reflect"
	"testing"
	"time"
)

func liveBurstEvaluationFixture() LiveSnapshot {
	snapshot := LiveSnapshot{Categories: "fixed-query-corpus", ObservedAt: "2026-10-01T00:00:00Z"}
	topic := LiveTopic{ID: "method", Name: "Fixed method"}
	for index := range 9 {
		start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, index, 0)
		period := start.Format("2006-01")
		snapshot.Periods = append(snapshot.Periods, LivePeriod{
			ID: period, Start: start.Format(time.DateOnly), EndExclusive: start.AddDate(0, 1, 0).Format(time.DateOnly),
			Complete: true, Population: 1000,
		})
		count := 1
		if index >= 6 {
			count = 10
		}
		topic.Series = append(topic.Series, StudyPeriod{Period: period, Count: count, Population: 1000})
	}
	snapshot.Topics = []LiveTopic{topic}
	return snapshot
}

func TestLiveBurstEvaluationSeparatesSignalFromFutureOutcome(t *testing.T) {
	snapshot := liveBurstEvaluationFixture()
	before, _ := json.Marshal(snapshot, json.Deterministic(true))
	report, err := EvaluateLiveBurst(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 3 || report.Rows[0].Status != "evaluated" ||
		!*report.Rows[0].Signal.Flag || !*report.Rows[0].Sustained ||
		report.Burst.TP != 1 || report.SimpleGrowth.TP != 1 || report.Rows[1].Status != "right_censored" {
		t.Fatalf("incorrect dated comparison: %+v", report)
	}
	after, _ := json.Marshal(snapshot, json.Deterministic(true))
	if string(before) != string(after) {
		t.Fatal("evaluation mutated source snapshot")
	}
	snapshot.Topics[0].Series[7].Count, snapshot.Topics[0].Series[8].Count = 100, 0
	changed, err := EvaluateLiveBurst(snapshot)
	if err != nil || !reflect.DeepEqual(changed.Rows[0].Signal, report.Rows[0].Signal) ||
		*changed.Rows[0].Sustained || changed.Burst.FP != 1 {
		t.Fatalf("future outcome leaked into the earlier detector: %+v, %v", changed, err)
	}
	snapshot.Periods[8].Complete = false
	censored, err := EvaluateLiveBurst(snapshot)
	if err != nil || censored.Rows[0].Sustained != nil ||
		censored.Rows[0].Status != "right_censored" || censored.Burst.Evaluated != 0 {
		t.Fatalf("incomplete follow-up became a negative label: %+v, %v", censored, err)
	}
}

func TestLiveBurstEvaluationRejectsGapsAndKeepsSampledUnknown(t *testing.T) {
	snapshot := liveBurstEvaluationFixture()
	snapshot.Topics[0].Analysis = &LiveTopicAnalysis{Discovery: &LiveDiscovery{Sampled: true}}
	report, err := EvaluateLiveBurst(snapshot)
	if err != nil || report.Burst.Evaluated != 0 || report.Rows[0].Signal.Flag != nil ||
		report.Rows[0].Sustained != nil || report.Rows[0].Signal.Status != "sampled_membership" {
		t.Fatalf("sample inflated into a measured population: %+v, %v", report, err)
	}
	snapshot.Periods[2].Start = "2026-03-02"
	if _, err := EvaluateLiveBurst(snapshot); err == nil {
		t.Fatal("ambiguous/gapped calendar accepted")
	}
}

func TestLiveBurstEvaluationCountsDisagreementAgainstFollowup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts []int
		want   LiveBurstIncremental
	}{
		{"additional true", []int{1, 1, 1, 1, 1, 15, 25, 20, 20}, LiveBurstIncremental{AdditionalTrue: 1}},
		{"additional false", []int{1, 1, 1, 1, 1, 15, 25, 1, 1}, LiveBurstIncremental{AdditionalFalse: 1}},
		{"missed true", []int{10, 10, 10, 10, 10, 1, 5, 100, 100}, LiveBurstIncremental{MissedTrue: 1}},
		{"avoided false", []int{10, 10, 10, 10, 10, 1, 5, 1, 1}, LiveBurstIncremental{AvoidedFalse: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := liveBurstEvaluationFixture()
			for i, count := range tc.counts {
				snapshot.Topics[0].Series[i].Count = count
			}
			report, err := EvaluateLiveBurst(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if report.Burst.Evaluated != 1 || report.SimpleGrowth.Evaluated != 1 || report.Incremental != tc.want {
				t.Fatalf("incremental outcomes = %+v, want %+v; report: %+v", report.Incremental, tc.want, report)
			}
		})
	}
}
