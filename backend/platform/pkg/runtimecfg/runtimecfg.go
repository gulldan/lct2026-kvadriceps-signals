// Package runtimecfg накладывает разрешённые настройки из БД поверх окружения; короткий кэш позволяет
// менять их без перезапуска. Адреса, контакты и секреты хранилище переопределений обходят.
package runtimecfg

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/rag-mvp/pkg/config"
	"github.com/example/rag-mvp/pkg/valkey"
)

const ValkeyKey = "rag:app_settings"

const (
	cacheTTL     = 3 * time.Second
	fetchTimeout = 400 * time.Millisecond
)

// Overrides: порядок — переопределение, окружение, умолчание; nil (или nil-клиент Valkey) читает только окружение.
type Overrides struct {
	kv      *valkey.Client
	mu      sync.Mutex
	vals    map[string]string
	fetched time.Time
}

func New(kv *valkey.Client) *Overrides { return &Overrides{kv: kv} }

func Publish(ctx context.Context, kv *valkey.Client, vals map[string]string) error {
	values := maps.Clone(vals)
	if values == nil {
		values = map[string]string{}
	}
	maps.DeleteFunc(values, func(key, _ string) bool {
		return key != "RAG_SETTINGS_REVISION" && !config.CanOverride(key)
	})
	return kv.PublishSettings(ctx, ValkeyKey, values)
}

func (o *Overrides) snapshot(ctx context.Context) map[string]string {
	if o == nil || o.kv == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if time.Since(o.fetched) < cacheTTL {
		return o.vals
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	var vals map[string]string
	if ok, err := o.kv.GetJSON(fetchCtx, ValkeyKey, &vals); err == nil {
		if !ok {
			vals = nil
		}
		o.vals = vals
	}
	o.fetched = time.Now()
	return o.vals
}

func (o *Overrides) Lookup(ctx context.Context, key string) (string, bool) {
	if key != "RAG_SETTINGS_REVISION" && !config.CanOverride(key) {
		return "", false
	}
	v, ok := o.snapshot(ctx)[key]
	return v, ok
}

func (o *Overrides) Get(ctx context.Context, key, def string) string {
	if v, ok := o.Lookup(ctx, key); ok && v != "" {
		return v
	}
	return config.Get(key, def)
}

func (o *Overrides) GetBool(ctx context.Context, key string, def bool) bool {
	if v, ok := o.Lookup(ctx, key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "t", "true", "yes", "on":
			return true
		case "0", "f", "false", "no", "off":
			return false
		}
	}
	return config.GetBool(key, def)
}

func (o *Overrides) GetInt(ctx context.Context, key string, def int) int {
	if v, ok := o.Lookup(ctx, key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return config.GetInt(key, def)
}

func (o *Overrides) GetFloat(ctx context.Context, key string, def float64) float64 {
	if v, ok := o.Lookup(ctx, key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return config.GetFloat(key, def)
}
