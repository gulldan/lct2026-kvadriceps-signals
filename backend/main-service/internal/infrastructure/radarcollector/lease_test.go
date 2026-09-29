package radarcollector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/main-service/internal/application"
)

func TestWriterOwnershipFencesReleaseAndStatus(t *testing.T) {
	server := miniredis.RunT(t)
	first, second := New(server.Addr(), ""), New(server.Addr(), "")
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	ctx, release, err := first.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = second.Acquire(t.Context())
	if !errors.Is(err, application.ErrLiveBusy) {
		t.Fatalf("competing writer: %v", err)
	}
	nested, nestedRelease, err := first.Acquire(ctx)
	if err != nil || nested != ctx {
		t.Fatal("same-owner nested job failed")
	}
	nestedRelease()
	if err = first.Save(ctx, &application.LiveCollectionStatus{Phase: "science"}); err != nil {
		t.Fatal(err)
	}
	// Потеря аренды и захват другим писателем без реального ожидания её истечения.
	server.Del(writerKey)
	next, nextRelease, err := second.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer nextRelease()
	if err = second.Save(next, &application.LiveCollectionStatus{Phase: "sources"}); err != nil {
		t.Fatal(err)
	}
	if err = first.Save(context.WithoutCancel(ctx), &application.LiveCollectionStatus{Phase: "idle"}); err == nil {
		t.Fatal("old writer overwrote new receipt")
	}
	release()
	if !server.Exists(writerKey) {
		t.Fatal("old release deleted new owner's lock")
	}
	state, err := second.Status(t.Context())
	if err != nil || state.Phase != "sources" {
		t.Fatal("last good receipt was lost")
	}
	if err := first.Save(t.Context(), &application.LiveCollectionStatus{}); err == nil {
		t.Fatal("unguarded status publication")
	}
}

func TestRecoveryIncludesConfigurationChangesAndSkipsFailures(t *testing.T) {
	base := application.LiveCollectorChannel{Status: "ready", ObservedAt: "2026-09-11T01:00:00Z", AttachedAt: "2026-09-11T01:00:00Z",
		SourceSet: "a", AttachedSourceSet: "a"}
	if needsRecovery([]application.LiveCollectorChannel{base}) {
		t.Fatal("unchanged source would repeat")
	}
	changed := base
	changed.SourceSet = "b"
	if !needsRecovery([]application.LiveCollectorChannel{changed}) {
		t.Fatal("changed source configuration not rebuilt")
	}
	newer := base
	newer.ObservedAt = "2026-09-11T02:00:00Z"
	if !needsRecovery([]application.LiveCollectorChannel{newer}) {
		t.Fatal("recovered source ignored")
	}
	newer.Error = "upstream unavailable"
	if needsRecovery([]application.LiveCollectorChannel{newer}) {
		t.Fatal("failed snapshot treated as recovered")
	}
	newer.Error = ""
	newer.Status = "collecting"
	if needsRecovery([]application.LiveCollectorChannel{newer}) {
		t.Fatal("unfinished snapshot treated as recovered")
	}
}

func TestCrashRecoveryExpiresLeaseAndReconcilesUnconfirmedRun(t *testing.T) {
	server := miniredis.RunT(t)
	old, replacement := New(server.Addr(), ""), New(server.Addr(), "")
	t.Cleanup(func() { _ = old.Close(); _ = replacement.Close() })
	ctx, release, err := old.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	server.FastForward(leaseTTL + time.Second)
	next, done, err := replacement.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, _, err = old.Acquire(ctx); err == nil {
		t.Fatal("old owner passed publication guard")
	}
	if err = replacement.Save(next, &application.LiveCollectionStatus{Phase: scienceJob}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	state := &application.LiveCollectionStatus{Phase: scienceJob, NextRunAt: now.Add(6 * time.Hour).Format(time.RFC3339),
		Runs: []application.LiveCollectorRun{{Kind: scienceJob, StartedAt: now.Add(-time.Minute).Format(time.RFC3339)}}}
	due := resumeStatus(state, application.LiveOverview{}, now, 6*time.Hour)
	if !due.Equal(now) || state.Runs[0].Error == "" || state.Runs[0].Success || state.LastSuccessAt != "" {
		t.Fatal("crashed writer reported success or postponed recovery")
	}
}
