package postgres

import (
	"testing"
	"time"
)

func TestDocumentStatsPlanNeverMakesTheDashboardWaitForAKnownCount(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cached         bool
		age            time.Duration
		serve, refresh bool
	}{
		{"first visit after restart waits for the count", false, 0, false, false},
		{"just counted", true, time.Second, true, false},
		{"older than half a minute refreshes in background", true, time.Minute, true, true},
		{"hours old is still served", true, 23 * time.Hour, true, true},
		{"older than a day is counted again", true, documentStatsStale, false, false},
	} {
		serve, refresh := documentStatsPlan(tc.cached, tc.age)
		if serve != tc.serve || refresh != tc.refresh {
			t.Errorf("%s: serve=%v refresh=%v", tc.name, serve, refresh)
		}
	}
	if documentStatsWarmEvery >= documentStatsStale {
		t.Fatal("warm-up must run before an admin count goes stale")
	}
}
