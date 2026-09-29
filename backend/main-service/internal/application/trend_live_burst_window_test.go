package application

import "testing"

func TestLiveBurstChecksOnlyItsActualHistoricalWindow(t *testing.T) {
	snapshot := liveBurstEvaluationFixture()
	snapshot.Periods[0].Population = 0
	snapshot.Topics[0].Series = snapshot.Topics[0].Series[1:]
	// База сентября — март–август; январь не участвует.
	result := liveBurst(snapshot.Topics[0], snapshot.Periods, snapshot.Categories, snapshot.Categories, nil)
	if result.Status != "measured" || result.Flag == nil || len(result.Baseline) != 6 || result.Baseline[0] != "2026-03" {
		t.Fatalf("unrelated older unknown blocked a complete measured window: %+v", result)
	}
	snapshot.Periods[2].Population = 0
	result = liveBurst(snapshot.Topics[0], snapshot.Periods, snapshot.Categories, snapshot.Categories, nil)
	if result.Status != "unknown_population" || result.Flag != nil {
		t.Fatalf("unknown inside the actual baseline became a measurement: %+v", result)
	}
}
