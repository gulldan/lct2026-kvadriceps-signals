package throttle_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/example/auth/internal/infrastructure/throttle"
	"github.com/example/auth/internal/platform/valkey"
)

func newThrottle(t *testing.T, cfg throttle.Config) (*throttle.Throttle, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	vk, err := valkey.New(context.Background(), mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("valkey.New: %v", err)
	}
	t.Cleanup(func() { _ = vk.Close() })
	return throttle.New(vk, cfg), mr
}

func defaultCfg() throttle.Config {
	return throttle.Config{MaxAttempts: 3, Window: time.Minute, Lockout: time.Minute}
}

func TestAllowedNoLockout(t *testing.T) {
	thr, _ := newThrottle(t, defaultCfg())
	ok, retry, err := thr.Allowed(context.Background(), "user:bob")
	if err != nil || !ok || retry != 0 {
		t.Fatalf("Allowed = (%v, %v, %v), want (true, 0, nil)", ok, retry, err)
	}
}

func TestRecordFailureLocksAfterThreshold(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	ctx := context.Background()

	for i := range 2 {
		if err := thr.RecordFailure(ctx, "user:bob"); err != nil {
			t.Fatalf("RecordFailure %d: %v", i, err)
		}
		if ok, _, _ := thr.Allowed(ctx, "user:bob"); !ok {
			t.Fatalf("locked out too early after %d failures", i+1)
		}
	}
	if ttl := mr.TTL("login_attempts:user:bob"); ttl <= 0 || ttl > time.Minute {
		t.Fatalf("attempts TTL = %s, want within the window", ttl)
	}

	if err := thr.RecordFailure(ctx, "user:bob"); err != nil {
		t.Fatalf("RecordFailure 3: %v", err)
	}
	ok, retry, err := thr.Allowed(ctx, "user:bob")
	if err != nil {
		t.Fatalf("Allowed after lockout: %v", err)
	}
	if ok {
		t.Fatal("expected lockout after reaching MaxAttempts")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry-after = %s, want a positive value within the lockout", retry)
	}
}

func TestResetClearsCounterAndLockout(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	ctx := context.Background()
	for i := range 3 {
		if err := thr.RecordFailure(ctx, "user:bob"); err != nil {
			t.Fatalf("RecordFailure %d: %v", i, err)
		}
	}
	if err := thr.Reset(ctx, "user:bob"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if mr.Exists("login_attempts:user:bob") || mr.Exists("login_lockout:user:bob") {
		t.Fatal("Reset must clear both the attempts and lockout keys")
	}
	if ok, _, _ := thr.Allowed(ctx, "user:bob"); !ok {
		t.Fatal("client must be allowed again after Reset")
	}
}

func TestAllowedStaleLockoutTreatedAsCleared(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	past := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	if err := mr.Set("login_lockout:user:bob", past); err != nil {
		t.Fatalf("seed lockout: %v", err)
	}
	ok, retry, err := thr.Allowed(context.Background(), "user:bob")
	if err != nil || !ok || retry != 0 {
		t.Fatalf("stale lockout Allowed = (%v, %v, %v), want (true, 0, nil)", ok, retry, err)
	}
}

func TestAllowedUnparsableLockoutFallsBackToConfig(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	if err := mr.Set("login_lockout:user:bob", "not-a-number"); err != nil {
		t.Fatalf("seed lockout: %v", err)
	}
	ok, retry, err := thr.Allowed(context.Background(), "user:bob")
	if err != nil {
		t.Fatalf("Allowed: %v", err)
	}
	if ok || retry != time.Minute {
		t.Fatalf("unparsable lockout = (%v, %v), want (false, 1m)", ok, retry)
	}
}

func TestAllowedFailsOpenWhenBackendDown(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	mr.Close()
	ok, retry, err := thr.Allowed(context.Background(), "user:bob")
	if !ok || retry != 0 {
		t.Fatalf("fail-open Allowed = (%v, %v), want (true, 0)", ok, retry)
	}
	if err == nil {
		t.Fatal("expected the backend error to be surfaced for logging")
	}
}

func TestRecordFailureErrorsWhenBackendDown(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	mr.Close()
	if err := thr.RecordFailure(context.Background(), "user:bob"); err == nil {
		t.Fatal("expected RecordFailure to error when backend is down")
	}
}

func TestResetErrorsWhenBackendDown(t *testing.T) {
	thr, mr := newThrottle(t, defaultCfg())
	mr.Close()
	if err := thr.Reset(context.Background(), "user:bob"); err == nil {
		t.Fatal("expected Reset to error when backend is down")
	}
}
