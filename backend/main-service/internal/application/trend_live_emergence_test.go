package application

import (
	"strings"
	"testing"
	"time"
)

func TestLiveEmergenceMapClassifiesByMedians(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 100
	}
	series := func(counts ...int) []StudyPeriod {
		rows := make([]StudyPeriod, len(periods))
		for i, count := range counts {
			rows[i] = StudyPeriod{Period: periods[i].ID, Population: 100, Count: count, Groups: count}
		}
		return rows
	}
	set := map[string][]StudyPeriod{
		"weak":       series(0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 4, 0),
		"strong":     series(5, 5, 6, 7, 8, 9, 10, 12, 14, 16, 18, 20, 22, 0),
		"known":      series(20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 100),
		"latent":     series(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0),
		"unmeasured": series(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 4, 0),
	}
	got := liveEmergenceMap(periods, set)
	for id, want := range map[string]string{"weak": liveEmergenceWeak, "strong": liveEmergenceStrong,
		"known": liveEmergenceKnown, "latent": liveEmergenceLatent, "unmeasured": liveEmergenceUnmeasured} {
		if got[id].Class != want {
			t.Errorf("%s: класс %s, ожидался %s (%s)", id, got[id].Class, want, got[id].Basis)
		}
		if got[id].Set.Size != 4 || got[id].PeriodsUsed != 13 || got[id].Method != liveEmergenceMethod {
			t.Errorf("%s: медианы или окно посчитаны не по набору: %+v", id, got[id])
		}
	}
	if !strings.HasPrefix(got["unmeasured"].Basis, "не измерено") || got["unmeasured"].DoVGrowth != nil {
		t.Errorf("тема с двумя точками получила прирост: %+v", got["unmeasured"])
	}
	if liveEmergenceRank(got["weak"]) >= liveEmergenceRank(got["known"]) ||
		liveEmergenceRank(nil) != liveEmergenceRank(got["unmeasured"]) {
		t.Error("порядок классов в выдаче нарушен")
	}
}
