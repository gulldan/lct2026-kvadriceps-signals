package valkey

import (
	"context"
	"fmt"
	"time"
)

// Сначала проверяем все значения: ошибка типа или переполнение не должны
// оставлять половину одной записи. Числа ограничены точной арифметикой Lua.
const usageScript = `
local limit = 9007199254740991
local next = {}
for i = 1, 4 do
  local field = ARGV[1] .. ({'req','pt','ct','cost'})[i]
  local old = tonumber(redis.call('HGET', KEYS[1], field) or '0')
  local delta = tonumber(ARGV[i+1])
  if not old or not delta or old < 0 or delta < 0 or old % 1 ~= 0 or delta % 1 ~= 0 or old + delta > limit then
    return redis.error_reply('invalid usage counter')
  end
  next[i] = {field, string.format('%.0f', old + delta)}
end
local minute = tonumber(redis.call('GET', KEYS[2]) or '0')
if not minute or minute < 0 or minute % 1 ~= 0 or minute >= limit then
  return redis.error_reply('invalid minute counter')
end
for i = 1, 4 do redis.call('HSET', KEYS[1], next[i][1], next[i][2]) end
redis.call('EXPIRE', KEYS[1], ARGV[6])
redis.call('SET', KEYS[2], string.format('%.0f', minute + 1), 'EX', ARGV[7])
return 1
`

func (c *Client) AddUsage(ctx context.Context, dayKey, minuteKey, prefix string,
	prompt, completion, cost int64, dayTTL, minuteTTL time.Duration,
) error {
	if err := c.rdb.Eval(ctx, usageScript, []string{dayKey, minuteKey},
		prefix, 1, prompt, completion, cost, int64(dayTTL/time.Second), int64(minuteTTL/time.Second)).Err(); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}
