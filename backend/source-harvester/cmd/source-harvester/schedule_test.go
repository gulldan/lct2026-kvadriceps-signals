package main

import (
	"testing"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/provider"
)

func TestRollingHistoryUsesCalendarMonthsAndResumes(t *testing.T) {
	s := scheduledScope{HistoryMonths: 13, Provider: "patent"}
	s.Scope.Query = "country:CN"
	a := scopeAt(s, time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC))
	b := scopeAt(s, time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	if a.Scope.From != "2025-08-01" || b.Scope.From != "2025-09-01" || b.Scope.Through != "2026-10-01" {
		t.Fatalf("history window does not roll by calendar month: %+v %+v", a.Scope, b.Scope)
	}
	if scopeCoverageKey("owner", "", a) != scopeCoverageKey("owner", "", b) {
		t.Fatal("rolling history lost its persistent scheduler checkpoint")
	}
	resumed := resumeScheduledWindow(b, scopeCoverage{Version: 1, Scope: a.Scope})
	if resumed.Scope != a.Scope {
		t.Fatal("unfinished history was abandoned on calendar rollover")
	}
}

func TestOneShotDoesNotClaimAPICollections(t *testing.T) {
	if extra := requestedScopes(t.Context(), nil, []scheduledScope{{Provider: "web"}}, true); len(extra) != 0 {
		t.Fatalf("one-shot run added unrelated collections: %+v", extra)
	}
}

func TestCurrentMonthScopeRollsOverInUTC(t *testing.T) {
	s := scheduledScope{CurrentMonth: true}
	a := scopeAt(s, time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC))
	b := scopeAt(s, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if a.Scope.From != "2026-09-01" || a.Scope.Through != "2026-09-30" ||
		b.Scope.From != "2026-10-01" || b.Scope.Through != "2026-10-01" {
		t.Fatalf("stale or future scope: %+v %+v", a, b)
	}
	a.CurrentMonth = false
	if scopeAt(a, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)).Scope != a.Scope {
		t.Fatal("explicit historical scope was changed")
	}
}

func TestCoverageReportsOnlyConfirmedCursorReset(t *testing.T) {
	job := harvest.Job{Lease: harvest.Lease{ScopeID: "scope", RunID: "run"}, Status: "failed"}
	coverage := coverageOutcome(scopeCoverage{}, scheduledScope{Provider: "patent"}, job, provider.ErrCursorExpired)
	if coverage.Cursor != "" || coverage.StopReason != "source_snapshot_changed; cursor reset, restart next batch" {
		t.Fatalf("confirmed reset receipt differs from checkpoint: %+v", coverage)
	}
	job.Status, job.Cursor = "running", "old-snapshot:200"
	coverage = coverageOutcome(scopeCoverage{}, scheduledScope{Provider: "patent"}, job, provider.ErrCursorExpired)
	if coverage.Cursor != job.Cursor || coverage.StopReason != "source_or_ingestion_failed; checkpoint retained" {
		t.Fatalf("unconfirmed reset receipt claims success: %+v", coverage)
	}
}

func TestIncompleteScopeFinishesBeforeAdvancingCurrentMonth(t *testing.T) {
	current := scopeAt(scheduledScope{CurrentMonth: true}, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	oldScope := scopeAt(scheduledScope{CurrentMonth: true}, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	previous := scopeCoverage{Version: 1, Scope: oldScope.Scope, Cursor: "next-page"}
	resumed := resumeScheduledWindow(current, previous)
	if resumed.Scope.Through != "2026-09-30" || resumed.Scope.From != "2026-09-01" {
		t.Fatalf("budget checkpoint skipped at rollover: %+v", resumed)
	}
	previous.Exhausted = true
	if resumeScheduledWindow(current, previous).Scope != current.Scope {
		t.Fatal("completed scope did not advance month")
	}
	if scopeCoverageKey("owner", "", current) != scopeCoverageKey("owner", "", resumed) {
		t.Fatal("calendar advancement changed scheduler manifest")
	}
	current.CurrentMonth = false
	resumed.CurrentMonth = false
	if scopeCoverageKey("owner", "", current) == scopeCoverageKey("owner", "", resumed) {
		t.Fatal("distinct fixed historical scopes collide")
	}
}
