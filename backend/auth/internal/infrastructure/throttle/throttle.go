package throttle

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/platform/valkey"
)

const (
	attemptsPrefix = "login_attempts:"
	lockoutPrefix  = "login_lockout:"
)

type Config struct {
	MaxAttempts int
	Window      time.Duration
	Lockout     time.Duration
}

type Throttle struct {
	vk  *valkey.Client
	cfg Config
}

var _ domain.LoginThrottle = (*Throttle)(nil)

// New: MaxAttempts ≤ 0 полностью отключает ограничение.
func New(vk *valkey.Client, cfg Config) *Throttle {
	return &Throttle{vk: vk, cfg: cfg}
}

func (t *Throttle) enabled() bool {
	return t.cfg.MaxAttempts > 0 && t.cfg.Window > 0 && t.cfg.Lockout > 0
}

func (t *Throttle) Allowed(ctx context.Context, key string) (bool, time.Duration, error) {
	if !t.enabled() {
		return true, 0, nil
	}
	raw, found, err := t.vk.Get(ctx, lockoutKey(key))
	if err != nil {
		// Сбой Valkey не должен запирать всех — пропускаем.
		return true, 0, fmt.Errorf("read lockout: %w", err)
	}
	if !found {
		return true, 0, nil
	}
	retryAfter := t.cfg.Lockout
	if unix, perr := strconv.ParseInt(raw, 10, 64); perr == nil {
		if d := time.Until(time.Unix(unix, 0)); d > 0 {
			retryAfter = d
		} else {
			// Метка уже просрочена (расхождение часов, устаревшее чтение) — считаем её снятой.
			return true, 0, nil
		}
	}
	return false, retryAfter, nil
}

func (t *Throttle) RecordFailure(ctx context.Context, key string) error {
	if !t.enabled() {
		return nil
	}
	n, err := t.vk.IncrTTL(ctx, attemptsKey(key), t.cfg.Window)
	if err != nil {
		return fmt.Errorf("increment attempts: %w", err)
	}
	if n >= int64(t.cfg.MaxAttempts) {
		expiry := time.Now().Add(t.cfg.Lockout).Unix()
		if serr := t.vk.Set(ctx, lockoutKey(key), strconv.FormatInt(expiry, 10), t.cfg.Lockout); serr != nil {
			return fmt.Errorf("set lockout: %w", serr)
		}
	}
	return nil
}

func (t *Throttle) Reset(ctx context.Context, key string) error {
	if !t.enabled() {
		return nil
	}
	if err := t.vk.Del(ctx, attemptsKey(key), lockoutKey(key)); err != nil {
		return fmt.Errorf("clear attempts: %w", err)
	}
	return nil
}

func attemptsKey(key string) string { return attemptsPrefix + key }
func lockoutKey(key string) string  { return lockoutPrefix + key }
