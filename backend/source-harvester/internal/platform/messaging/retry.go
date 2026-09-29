package messaging

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/example/source-harvester/internal/platform/contracts"
	amqp "github.com/rabbitmq/amqp091-go"
)

const attemptHeader = "x-application-attempt"

func retryDelays() [3]time.Duration {
	return [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
}

func retryQueue(queue string, attempt int) string { return queue + ".retry." + strconv.Itoa(attempt) }

func declareRetries(ch *amqp.Channel, queue string) error {
	for i, delay := range retryDelays() {
		args := amqp.Table{
			"x-queue-type": "quorum", "x-overflow": "reject-publish",
			"x-dead-letter-strategy": "at-least-once", "x-dead-letter-exchange": "",
			"x-dead-letter-routing-key": queue, "x-message-ttl": int64(delay / time.Millisecond),
		}
		if _, err := ch.QueueDeclare(retryQueue(queue, i+1), true, false, false, false, args); err != nil {
			return fmt.Errorf("declare delayed retry: %w", err)
		}
	}
	return nil
}

func messageAttempt(headers amqp.Table) int {
	value, exists := headers[attemptHeader]
	if !exists {
		return 0
	}
	var attempt int64
	switch number := value.(type) {
	case int32:
		attempt = int64(number)
	case int64:
		attempt = number
	case int:
		attempt = int64(number)
	default:
		return contracts.MaxAttempts - 1
	}
	if attempt < 0 || attempt >= contracts.MaxAttempts {
		return contracts.MaxAttempts - 1
	}
	return int(attempt)
}

func deliveryCopy(d amqp.Delivery) amqp.Publishing {
	headers := amqp.Table{}
	maps.Copy(headers, d.Headers)
	return amqp.Publishing{ContentType: d.ContentType, DeliveryMode: amqp.Persistent,
		Timestamp: time.Now().UTC(), Headers: headers, Body: d.Body, MessageId: d.MessageId,
		CorrelationId: d.CorrelationId}
}

func (c *Conn) retry(ctx context.Context, queue string, d amqp.Delivery, attempt int) {
	message := deliveryCopy(d)
	message.Headers[attemptHeader] = int32(attempt)
	c.move(ctx, "", retryQueue(queue, attempt), d, message)
}

func (c *Conn) park(ctx context.Context, queue string, d amqp.Delivery, cause error, class string) {
	message := deliveryCopy(d)
	message.Headers[contracts.HeaderFailureClass] = class
	message.Headers[contracts.HeaderFailureReason] = cause.Error()[:min(len(cause.Error()), 4096)]
	message.Headers[contracts.HeaderFailureQueue] = queue
	c.move(ctx, contracts.ExchangeDLX, "", d, message)
}

// Исходное сообщение подтверждается только после принятия копии брокером.
// При неопределённом результате возвращаем оригинал и пересоздаём соединение.
func (c *Conn) move(ctx context.Context, exchange, key string, d amqp.Delivery, message amqp.Publishing) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := errors.New("retry publisher unavailable")
	if c.parker != nil {
		err = c.parker.publish(ctx, exchange, key, message)
	}
	if err == nil {
		err = d.Ack(false)
	} else {
		_ = d.Nack(false, true)
	}
	if err != nil {
		c.log.Error().Err(err).Msg("delivery transfer unconfirmed; reconnecting")
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}
}
