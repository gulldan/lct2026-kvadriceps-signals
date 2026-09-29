package application

import (
	"testing"
	"time"
)

func TestLiveRollingComparisonIncludesSeptemberAndEqualDays(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 19, 0, 0, 0, time.UTC))
	papers := []StudyPaper{
		{ID: "before", Date: "2025-09-07", Authors: []string{"Earlier"}},
		{ID: "prior", Date: "2025-09-08", Authors: []string{"A"}},
		{ID: "boundary", Date: "2026-03-11", Authors: []string{"A"}},
		{ID: "today", Date: "2026-09-10", Authors: []string{"New"}},
		{ID: "future", Date: "2026-09-11", Authors: []string{"Future"}},
	}
	c := liveCompare(periods, papers)
	if c == nil || c.PriorStart != "2025-09-08" || c.RecentStart != "2026-03-11" || c.EndExclusive != "2026-09-11" ||
		c.Days != 184 || c.Prior != 1 || c.Current != 2 || c.Fresh != 1 {
		t.Fatalf("wrong dated comparison: %+v", c)
	}
	for _, date := range []string{"2026-08-30", "2024-08-30", "2026-12-31", "2026-01-01"} {
		now, err := time.Parse(time.DateOnly, date)
		if err != nil {
			t.Fatal(err)
		}
		p := livePeriods(now)
		c := liveCompare(p, nil)
		if c == nil {
			t.Fatalf("no comparison at %s", date)
		}
		a, b, end := liveDay(c.PriorStart), liveDay(c.RecentStart), liveDay(c.EndExclusive)
		if b.Sub(a) != end.Sub(b) || c.EndExclusive != now.AddDate(0, 0, 1).Format(time.DateOnly) {
			t.Fatalf("unequal or stale %s: %+v", date, c)
		}
		if date == "2026-08-30" && c.RecentStart != "2026-02-28" {
			t.Fatalf("month end not clamped: %+v", c)
		}
	}
}
