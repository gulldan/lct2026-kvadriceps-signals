package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"github.com/example/rag-mvp/pkg/contracts"
	"github.com/example/rag-mvp/pkg/logger"
)

const protoSchemaVersion = "v1"

type Recorder interface {
	RecordMessage(queue, outcome string, seconds float64)
}

type confirmedPublisher interface {
	publish(context.Context, string, string, amqp.Publishing) error
}

type Conn struct {
	conn     *amqp.Connection
	log      zerolog.Logger
	parkOnce sync.Once
	parker   confirmedPublisher
	parkErr  error
}

// Dial повторяет попытки, чтобы сервис пережил брокер, который стартует позже него.
func Dial(ctx context.Context, url string, log zerolog.Logger) (*Conn, error) {
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		conn, err := amqp.Dial(url)
		if err == nil {
			log.Info().Msg("connected to rabbitmq")
			return &Conn{conn: conn, log: log}, nil
		}
		lastErr = err
		log.Warn().Int("attempt", attempt).Err(err).Msg("rabbitmq not ready, retrying")
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("dial rabbitmq: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("dial rabbitmq: %w", lastErr)
}

func (c *Conn) Close() error {
	if c.conn == nil {
		return nil
	}
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("close rabbitmq: %w", err)
	}
	return nil
}

func (c *Conn) DeclareTopology() error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	for _, ex := range []struct{ name, kind string }{
		{contracts.ExchangeIngestion, "topic"},
		{contracts.ExchangeEvents, "fanout"},
		{contracts.ExchangeDLX, "fanout"},
	} {
		if err = ch.ExchangeDeclare(ex.name, ex.kind, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", ex.name, err)
		}
	}

	if _, err = ch.QueueDeclare(contracts.QueueDead, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead queue: %w", err)
	}
	if err = ch.QueueBind(contracts.QueueDead, "", contracts.ExchangeDLX, false, nil); err != nil {
		return fmt.Errorf("bind dead queue: %w", err)
	}

	args := amqp.Table{"x-dead-letter-exchange": contracts.ExchangeDLX}
	for queue, key := range contracts.ParserQueues() {
		if _, err = ch.QueueDeclare(queue, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare queue %s: %w", queue, err)
		}
		if err = ch.QueueBind(queue, key, contracts.ExchangeIngestion, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", queue, err)
		}
		if err = declareRetries(ch, queue); err != nil {
			return err
		}
	}
	return nil
}

// Publisher защищает канал мьютексом: каналы amqp091 не выдерживают конкурентной записи.
type Publisher struct {
	ch       *amqp.Channel
	mu       sync.Mutex
	log      zerolog.Logger
	returned <-chan amqp.Return
}

func (c *Conn) NewPublisher() (*Publisher, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open publish channel: %w", err)
	}
	// Без режима подтверждений отброшенная брокером публикация теряется молча, и документ навсегда
	// застревает в текущем статусе.
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publish confirms: %w", err)
	}
	return &Publisher{ch: ch, mu: sync.Mutex{}, log: c.log, returned: ch.NotifyReturn(make(chan amqp.Return, 1))}, nil
}

func (p *Publisher) PublishProto(ctx context.Context, exchange, routingKey string, msg proto.Message) error {
	body, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal proto message: %w", err)
	}
	return p.publish(ctx, exchange, routingKey, amqp.Publishing{
		ContentType:  "application/x-protobuf",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Headers: amqp.Table{
			"x-proto-type":    string(msg.ProtoReflect().Descriptor().FullName()),
			"x-proto-version": protoSchemaVersion,
		},
		Body: body,
	})
}

func (p *Publisher) publish(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	if p == nil {
		return errors.New("publisher is not open")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	dc, err := p.ch.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		routingKey,
		exchange != contracts.ExchangeEvents,
		false,
		msg,
	)
	if err != nil {
		return fmt.Errorf("publish to %s/%s: %w", exchange, routingKey, err)
	}
	acked, err := dc.WaitContext(ctx)
	if err != nil {
		_ = p.ch.Close()
		return fmt.Errorf("await confirm for %s/%s: %w", exchange, routingKey, err)
	}
	select {
	case returned := <-p.returned:
		return fmt.Errorf("unroutable publish to %s/%s: %d", exchange, routingKey, returned.ReplyCode)
	default:
	}
	if !acked {
		return fmt.Errorf("publish to %s/%s nacked by broker", exchange, routingKey)
	}
	return nil
}

func (p *Publisher) Close() error {
	if err := p.ch.Close(); err != nil {
		return fmt.Errorf("close publish channel: %w", err)
	}
	return nil
}

type HandlerFunc func(ctx context.Context, body []byte) error

type ConsumerConfig struct {
	Queue    string
	Workers  int
	Prefetch int
	// Ноль — без дедлайна; иначе зависший бэкенд освобождает слот воркера, а не держит его вечно.
	HandleTimeout time.Duration
	Recorder      Recorder
}

func (c *Conn) Consume(ctx context.Context, cfg ConsumerConfig, handle HandlerFunc) error {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.Prefetch <= 0 {
		cfg.Prefetch = cfg.Workers
	}
	c.parkOnce.Do(func() { c.parker, c.parkErr = c.NewPublisher() })
	if c.parkErr != nil {
		return c.parkErr
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("open consume channel: %w", err)
	}
	defer func() { _ = ch.Close() }()
	// При рестарте брокера deliveries закрывается молча; без этого процесс висел бы потребителем
	// без подписки, и его никто бы не перезапустил.
	closed := make(chan *amqp.Error, 1)
	ch.NotifyClose(closed)

	if err = ch.Qos(cfg.Prefetch, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}
	consumerTag := "worker-" + uuid.NewV4().String()
	deliveries, err := ch.Consume(cfg.Queue, consumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", cfg.Queue, err)
	}
	c.log.Info().Str("queue", cfg.Queue).Int("workers", cfg.Workers).Msg("consuming")

	var wg sync.WaitGroup
	for range cfg.Workers {
		wg.Go(func() {
			for d := range deliveries {
				if ctx.Err() != nil {
					// Отложенное закрытие канала вернёт все нетронутые доставки.
					return
				}
				c.dispatch(ctx, cfg, d, handle)
			}
		})
	}
	select {
	case <-ctx.Done():
		_ = ch.Cancel(consumerTag, false)
		wg.Wait()
		return nil
	case amqpErr := <-closed:
		wg.Wait()
		if amqpErr != nil {
			return fmt.Errorf("consume channel closed: %w", amqpErr)
		}
		return errors.New("consume channel closed")
	}
}

func (c *Conn) dispatch(ctx context.Context, cfg ConsumerConfig, d amqp.Delivery, handle HandlerFunc) {
	if ctx.Err() != nil {
		_ = d.Nack(false, true)
		return
	}
	start := time.Now()
	attempt := messageAttempt(d.Headers)
	mctx := contracts.WithAttempt(logger.Into(ctx, c.log), attempt)
	if cfg.HandleTimeout > 0 {
		var cancel context.CancelFunc
		mctx, cancel = context.WithTimeout(mctx, cfg.HandleTimeout)
		defer cancel()
	}
	err := handle(mctx, d.Body)
	const outcomeRetry = "retry"
	outcome := "ok"
	switch {
	case err == nil:
		_ = d.Ack(false)
	case ctx.Err() != nil:
		// Рестарт сервиса — не ядовитое сообщение и не таймаут обработки.
		outcome = outcomeRetry
		_ = d.Nack(false, true)
	case errors.Is(err, contracts.ErrDurability):
		outcome = outcomeRetry
		_ = d.Nack(false, true)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	case contracts.IsUnrecoverable(err):
		// Тело, не прошедшее декодирование или схему, упадёт так же и при повторе.
		outcome = "review"
		c.log.Error().Err(err).Str("queue", cfg.Queue).Msg("handling failed unrecoverably; sending for review")
		c.park(ctx, cfg.Queue, d, err, contracts.FailureReview)
	case attempt < contracts.MaxAttempts-1:
		outcome = outcomeRetry
		c.retry(ctx, cfg.Queue, d, attempt+1)
	default:
		outcome = "error"
		c.log.Error().Err(err).Str("queue", cfg.Queue).Msg("handling failed after retry; dead-lettering")
		c.park(ctx, cfg.Queue, d, err, contracts.FailureTransient)
	}
	if cfg.Recorder != nil {
		cfg.Recorder.RecordMessage(cfg.Queue, outcome, time.Since(start).Seconds())
	}
}

// ConsumeEvents подтверждает доставки автоматически: потеря события не критична.
func (c *Conn) ConsumeEvents(ctx context.Context, handle HandlerFunc) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("open events channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return fmt.Errorf("declare events queue: %w", err)
	}
	if err = ch.QueueBind(q.Name, "", contracts.ExchangeEvents, false, nil); err != nil {
		return fmt.Errorf("bind events queue: %w", err)
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume events: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				// Канал закрылся не по нашей инициативе — сообщаем наверх, иначе живые обновления молча
				// умрут до рестарта процесса.
				return errors.New("events channel closed")
			}
			if herr := handle(logger.Into(ctx, c.log), d.Body); herr != nil {
				c.log.Warn().Err(herr).Msg("events handler error")
			}
		}
	}
}

type DeadMessage struct {
	Class     string    `json:"class"`
	Reason    string    `json:"reason"`
	Queue     string    `json:"queue"`
	Timestamp time.Time `json:"timestamp,omitzero"`
	Bytes     int       `json:"bytes"`
}

// PeekDead возвращает все прочитанные сообщения обратно: прочитать ещё не значит решить.
func (c *Conn) PeekDead(limit int) ([]DeadMessage, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open review channel: %w", err)
	}
	defer func() { _ = ch.Close() }()
	out := make([]DeadMessage, 0, limit)
	for range limit {
		d, ok, getErr := ch.Get(contracts.QueueDead, false)
		if getErr != nil {
			return out, fmt.Errorf("read review queue: %w", getErr)
		}
		if !ok {
			break
		}
		out = append(out, DeadMessage{
			Class:     header(d.Headers, contracts.HeaderFailureClass),
			Reason:    header(d.Headers, contracts.HeaderFailureReason),
			Queue:     header(d.Headers, contracts.HeaderFailureQueue),
			Timestamp: d.Timestamp,
			Bytes:     len(d.Body),
		})
		// Доставки остаются неподтверждёнными, пока Close не вернёт их разом: requeue внутри цикла
		// читал бы одно и то же головное сообщение.
	}
	return out, nil
}

func header(table amqp.Table, name string) string {
	if value, ok := table[name].(string); ok {
		return value
	}
	return ""
}
