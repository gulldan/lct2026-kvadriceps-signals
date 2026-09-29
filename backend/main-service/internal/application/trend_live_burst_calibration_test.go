package application

import (
	"encoding/json/v2"
	"math"
	"reflect"
	"testing"
)

func TestLiveBurstBinomialTail(t *testing.T) {
	for _, test := range []struct {
		n, threshold int
		p, want      float64
	}{
		{1, 1, .3, .3}, {2, 1, .5, .75}, {3, 2, .5, .5}, {100, 101, .5, 0},
		{20, 10, 0, 0}, {20, 10, 1, 1}, {10, 0, .5, 1}, {10001, 5001, .5, .5},
	} {
		if got := liveBinomialTail(test.n, test.threshold, test.p); math.Abs(got-test.want) > 1e-10 {
			t.Fatalf("tail(%d,%d,%g)=%g, want %g", test.n, test.threshold, test.p, got, test.want)
		}
	}
}

func TestLiveBurstCalibrationUsesOnlyPastAndNativeThresholds(t *testing.T) {
	snapshot := liveBurstEvaluationFixture()
	before, _ := json.Marshal(snapshot, json.Deterministic(true))
	calibration, err := CalibrateLiveBurst(snapshot)
	if err != nil || len(calibration.Windows) != 3 ||
		calibration.Windows[0].BurstMinimum != 5 || calibration.Windows[0].GrowthMinimum != 5 {
		t.Fatalf("unexpected native thresholds: %+v, %v", calibration, err)
	}
	first := calibration.Windows[0]
	if first.Scenarios[0].Burst >= first.Scenarios[1].Burst || first.Scenarios[1].Burst >= first.Scenarios[2].Burst {
		t.Fatal("abrupt increase did not monotonically increase detection probability")
	}
	after, _ := json.Marshal(snapshot, json.Deterministic(true))
	if string(before) != string(after) {
		t.Fatal("calibration mutated source counts")
	}
	snapshot.Topics[0].Series[7].Count, snapshot.Topics[0].Series[8].Count = 200, 400
	changed, err := CalibrateLiveBurst(snapshot)
	if err != nil || !reflect.DeepEqual(changed.Windows[0], first) {
		t.Fatal("future counts leaked into conditional calibration")
	}
}
