package main

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSchedulerProgressFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	if err := checkProgress(path, time.Now()); err == nil {
		t.Fatal("missing progress was healthy")
	}
	if err := recordProgress(path, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := checkProgress(path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := checkProgress(path, time.Now().Add(2*time.Minute)); err == nil {
		t.Fatal("stalled scheduler was healthy")
	}
	raw, err := json.Marshal(progress{PID: 0, Deadline: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkProgress(path, time.Now()); err == nil {
		t.Fatal("missing process was healthy")
	}
}

func TestReadinessBrokerFailureIsBoundedAndSanitized(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://sensitive-user:sensitive-password@127.0.0.1:1/")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := brokerReady(ctx); err == nil || err.Error() != "broker is not ready" {
		t.Fatalf("readiness error must be safe and explicit, got %v", err)
	}
}
