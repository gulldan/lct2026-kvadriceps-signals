package valkey

import (
	"context"
	"errors"
	"fmt"
)

// EnqueueWork кладёт каждый принятый запуск в очередь, а не затирает предыдущий.
func (c *Client) EnqueueWork(ctx context.Context, key, request string) error {
	accepted, err := c.rdb.Eval(ctx, `
 if redis.call('LLEN',KEYS[2])>=100 then return 0 end
 redis.call('LPUSH',KEYS[2],ARGV[1]);redis.call('SET',KEYS[1],ARGV[1]);return 1`, []string{key, key + ":pending"}, request).Int()
	if err != nil {
		return fmt.Errorf("enqueue maintenance work: %w", err)
	}
	if accepted != 1 {
		return errors.New("maintenance queue is full")
	}
	return nil
}
