package load_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/example/llm-service/internal/infrastructure/load"
	"github.com/example/llm-service/internal/platform/valkey"
)

func newMarker(t *testing.T) (*load.Marker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client, err := valkey.New(context.Background(), mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() {
		if cerr := client.Close(); cerr != nil {
			t.Errorf("close: %v", cerr)
		}
	})
	return load.New(client), mr
}

func gauge(t *testing.T, mr *miniredis.Miniredis) string {
	t.Helper()
	v, err := mr.Get(load.Key)
	if err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	return v
}

func TestStartedFinishedAdjustGauge(t *testing.T) {
	m, mr := newMarker(t)
	ctx := context.Background()
	m.QueryStarted(ctx)
	m.QueryStarted(ctx)
	if got := gauge(t, mr); got != "2" {
		t.Fatalf("gauge after two starts = %q, want 2", got)
	}
	if mr.TTL(load.Key) <= 0 {
		t.Fatal("QueryStarted should refresh the gauge TTL")
	}
	m.QueryFinished(ctx)
	m.QueryFinished(ctx)
	if got := gauge(t, mr); got != "0" {
		t.Fatalf("gauge after matching finishes = %q, want 0", got)
	}
}

func TestMarkerBestEffortOnFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := valkey.New(context.Background(), mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if cerr := client.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	m := load.New(client)
	ctx := context.Background()
	// Проверяется лишь, что вызовы не паникуют, хотя каждый запрос к Valkey падает.
	m.QueryStarted(ctx)
	m.QueryFinished(ctx)
}
