package analysisdoc_test

import (
	"testing"
	"time"

	"github.com/example/archive-worker/internal/platform/analysisdoc"
	"github.com/example/archive-worker/internal/platform/sourcedoc"
)

func TestHistoryModeVocabularyKeepsOldSnapshotsReadable(t *testing.T) {
	for stored, want := range map[string]string{
		"as_observed":                      analysisdoc.ModeAsOf,
		"as_of":                            analysisdoc.ModeAsOf,
		"reconstruction":                   analysisdoc.ModeRetrospective,
		"retrospective_current_membership": analysisdoc.ModeRetrospective,
	} {
		if got := analysisdoc.NormalizeMode(stored); got != want {
			t.Fatalf("a snapshot stored as %q reads as %q, want %q", stored, got, want)
		}
	}
	for _, unknown := range []string{"", "AS_OF", "historical", "as of", "retrospective"} {
		if got := analysisdoc.NormalizeMode(unknown); got != "" {
			t.Fatalf("mode %q was accepted as %q", unknown, got)
		}
	}
}

func TestOldRequestStillSelectsItsRule(t *testing.T) {
	cutoff := "2026-01-01T00:00:00Z"
	// Доступен до среза, но замечен после него — именно этот случай решает режим истории.
	available := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	late := sourcedoc.Source{ObservedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), AvailableAt: &available}
	for mode, admitted := range map[string]bool{
		"reconstruction":                   true,
		"retrospective_current_membership": true,
		"as_observed":                      false,
		"as_of":                            false,
	} {
		_, limits, reason := analysisdoc.EligibleSource(late, analysisdoc.Request{Mode: mode, Cutoff: cutoff})
		if admitted && reason != "" {
			t.Fatalf("mode %q refused a late observation it used to admit: %s", mode, reason)
		}
		if !admitted && reason != "not_observed_at_cutoff" {
			t.Fatalf("mode %q admitted a late observation: %q", mode, reason)
		}
		if admitted && len(limits) == 0 {
			t.Fatalf("mode %q admitted a late observation without saying so", mode)
		}
	}
}

func TestAnalysisKindDefaultsToTheBrief(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	base := analysisdoc.Request{OwnerID: "6f1c1f3a-0d5a-4a6a-9a4a-2a7c9c2f1b01",
		RequestID: "9b3b2f5e-1c2d-4a3b-8c9d-0e1f2a3b4c5d", Query: "protonoobmennaya membrana",
		Start: "2025-01-01", Cutoff: "2026-01-01T00:00:00Z", Mode: "as_observed"}
	silent, err := base.Normalize(now)
	if err != nil || silent.AnalysisKind != analysisdoc.KindBriefV1 || silent.Mode != analysisdoc.ModeAsOf {
		t.Fatalf("a request naming no kind: %+v %v", silent, err)
	}
	asked := base
	asked.AnalysisKind = analysisdoc.KindBriefV1
	second, err := asked.Normalize(now)
	if err != nil || second.AnalysisKind != analysisdoc.KindBriefV1 {
		t.Fatalf("a request naming v1: %+v %v", second, err)
	}
	unknown := base
	unknown.AnalysisKind = "technology-trends-v2"
	if _, err = unknown.Normalize(now); err == nil {
		t.Fatal("a separate v2 path was accepted")
	}
	broken := base
	broken.Mode = "whenever"
	if _, err = broken.Normalize(now); err == nil {
		t.Fatal("an unknown history mode was accepted")
	}
}
