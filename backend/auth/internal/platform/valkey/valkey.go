package valkey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/example/auth/internal/platform/jsonx"
)

type Client struct {
	rdb *redis.Client
}

func New(ctx context.Context, addr, password string, db int) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping valkey: %w", err)
	}
	return &Client{rdb: rdb}, nil
}

func (c *Client) Close() error {
	if err := c.rdb.Close(); err != nil {
		return fmt.Errorf("close valkey: %w", err)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping valkey: %w", err)
	}
	return nil
}

func (c *Client) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := jsonx.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode value: %w", err)
	}
	if serr := c.rdb.Set(ctx, key, b, ttl).Err(); serr != nil {
		return fmt.Errorf("valkey set %s: %w", key, serr)
	}
	return nil
}

// PublishSettings защищает читателей от запоздавшего издателя старой ревизии.
func (c *Client) PublishSettings(ctx context.Context, key string, values map[string]string) error {
	b, err := jsonx.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	const script = `
local incoming = cjson.decode(ARGV[1])
local revision = tonumber(incoming.RAG_SETTINGS_REVISION)
if not revision then return redis.error_reply('settings revision required') end
local current = redis.call('GET', KEYS[1])
if current then
  local old = tonumber(cjson.decode(current).RAG_SETTINGS_REVISION) or 0
  if old > revision then return 0 end
  -- Сравнение завершено; сохраняем только актуальный снимок.
end
redis.call('SET', KEYS[1], ARGV[1])
return 1
`
	if err = c.rdb.Eval(ctx, script, []string{key}, b).Err(); err != nil {
		return fmt.Errorf("publish settings: %w", err)
	}
	return nil
}

func (c *Client) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	b, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("valkey get %s: %w", key, err)
	}
	if uerr := jsonx.Unmarshal(b, dest); uerr != nil {
		return false, fmt.Errorf("decode value: %w", uerr)
	}
	return true, nil
}

func (c *Client) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	if err := c.rdb.Set(ctx, key, val, ttl).Err(); err != nil {
		return fmt.Errorf("valkey set %s: %w", key, err)
	}
	return nil
}

func (c *Client) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, key, val, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("valkey setnx %s: %w", key, err)
	}
	return ok, nil
}

func (c *Client) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("valkey get %s: %w", key, err)
	}
	return v, true, nil
}

// GetDel однократно забирает значение, не оставляя окна для повторного использования.
func (c *Client) GetDel(ctx context.Context, key string) (string, bool, error) {
	v, err := c.rdb.GetDel(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("valkey consume: %w", err)
	}
	return v, true, nil
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("valkey del: %w", err)
	}
	return nil
}

func (c *Client) LPush(ctx context.Context, key string, values ...string) error {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	if err := c.rdb.LPush(ctx, key, args...).Err(); err != nil {
		return fmt.Errorf("valkey lpush %s: %w", key, err)
	}
	return nil
}

func (c *Client) LTrim(ctx context.Context, key string, start, stop int64) error {
	if err := c.rdb.LTrim(ctx, key, start, stop).Err(); err != nil {
		return fmt.Errorf("valkey ltrim %s: %w", key, err)
	}
	return nil
}

func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	values, err := c.rdb.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("valkey lrange %s: %w", key, err)
	}
	return values, nil
}

// IncrTTL обновляет TTL, чтобы счётчик сам восстановился, если держатель умер, не уменьшив его.
func (c *Client) IncrTTL(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := c.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("valkey incr %s: %w", key, err)
	}
	if ttl > 0 {
		if eerr := c.rdb.Expire(ctx, key, ttl).Err(); eerr != nil {
			return n, fmt.Errorf("valkey expire %s: %w", key, eerr)
		}
	}
	return n, nil
}

func (c *Client) HIncrBy(ctx context.Context, key, field string, n int64) error {
	if err := c.rdb.HIncrBy(ctx, key, field, n).Err(); err != nil {
		return fmt.Errorf("valkey hincrby %s: %w", key, err)
	}
	return nil
}

func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	m, err := c.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("valkey hgetall %s: %w", key, err)
	}
	return m, nil
}

func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := c.rdb.Expire(ctx, key, ttl).Err(); err != nil {
		return fmt.Errorf("valkey expire %s: %w", key, err)
	}
	return nil
}

// Decr может увести счётчик ниже нуля, если TTL истёк между парными Incr/Decr; читатели прижимают к нулю.
func (c *Client) Decr(ctx context.Context, key string) (int64, error) {
	n, err := c.rdb.Decr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("valkey decr %s: %w", key, err)
	}
	return n, nil
}

func (c *Client) GetInt(ctx context.Context, key string) (int64, error) {
	n, err := c.rdb.Get(ctx, key).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("valkey get %s: %w", key, err)
	}
	return n, nil
}
