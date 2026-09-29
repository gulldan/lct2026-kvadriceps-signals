package radarcollector

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/redis/go-redis/v9"
)

const statusKey = "trends:collector:status:v1"
const writerKey = "trends:collector:writer:v1"
const jobLimit = 150 * time.Minute
const leaseTTL = 45 * time.Second
const idlePhase = "idle"

type Coordinator struct{ client *redis.Client }
type leaseOwner struct{}
type leaseClaim struct {
	coordinator *Coordinator
	owner       string
}

func New(address, password string) *Coordinator {
	return &Coordinator{client: redis.NewClient(&redis.Options{Addr: address, Password: password})}
}

func (c *Coordinator) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("close radar coordinator: %w", err)
	}
	return nil
}

// Acquire продлевает короткую аренду с проверкой владельца, пока идёт ограниченная задача. Жёсткое
// падение освобождает писателя за 45 секунд; публикацию дополнительно ограждает S3.
func (c *Coordinator) Acquire(parent context.Context) (context.Context, func(), error) {
	if claim, ok := parent.Value(leaseOwner{}).(leaseClaim); ok && claim.coordinator == c {
		if err := parent.Err(); err != nil {
			return parent, nil, fmt.Errorf("radar job context: %w", err)
		}
		owner, err := c.client.Get(parent, writerKey).Result()
		if err != nil || owner != claim.owner {
			return parent, nil, errors.New("radar writer lease lost")
		}
		return parent, func() {}, nil
	}
	ctx, cancel := context.WithTimeout(parent, jobLimit)
	owner := rand.Text()
	locked, err := c.client.SetNX(ctx, writerKey, owner, leaseTTL).Result()
	if err != nil || !locked {
		cancel()
		if err != nil {
			return parent, nil, fmt.Errorf("radar writer lease: %w", err)
		}
		return parent, nil, application.ErrLiveBusy
	}
	ctx, lost := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go c.renew(ctx, lost, owner, done)
	return context.WithValue(ctx, leaseOwner{}, leaseClaim{coordinator: c, owner: owner}), func() {
		cancel()
		<-done
		releaseCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = c.client.Eval(releaseCtx, `if redis.call('get', KEYS[1]) == ARGV[1] then
            return redis.call('del', KEYS[1]) else return 0 end`, []string{writerKey}, owner).Err()
	}, nil
}

func (c *Coordinator) renew(ctx context.Context, lost context.CancelCauseFunc, owner string, done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			checkCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			renewed, err := c.client.Eval(checkCtx, `if redis.call('get', KEYS[1]) == ARGV[1] then
                        return redis.call('pexpire', KEYS[1], ARGV[2]) else return 0 end`,
				[]string{writerKey}, owner, leaseTTL.Milliseconds()).Int()
			stop()
			if err != nil || renewed != 1 {
				lost(errors.New("radar writer lease lost"))
				return
			}
		}
	}
}

func (c *Coordinator) Status(ctx context.Context) (*application.LiveCollectionStatus, error) {
	raw, err := c.client.Get(ctx, statusKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return &application.LiveCollectionStatus{Phase: idlePhase}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("radar status: %w", err)
	}
	var result application.LiveCollectionStatus
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("radar status: %w", err)
	}
	return &result, nil
}

// Save атомарно пишет статус только от текущего владельца аренды.
func (c *Coordinator) Save(ctx context.Context, state *application.LiveCollectionStatus) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("save radar status: %w", err)
	}
	claim, ok := ctx.Value(leaseOwner{}).(leaseClaim)
	if !ok || claim.coordinator != c {
		return errors.New("radar status write requires its owner lease")
	}
	saved, err := c.client.Eval(ctx, `if redis.call('get', KEYS[1]) == ARGV[1] then
        redis.call('set', KEYS[2], ARGV[2]); return 1 else return 0 end`,
		[]string{writerKey, statusKey}, claim.owner, raw).Int()
	if err != nil {
		return fmt.Errorf("save radar status: %w", err)
	}
	if saved != 1 {
		return errors.New("radar writer lease lost before status publication")
	}
	return nil
}
