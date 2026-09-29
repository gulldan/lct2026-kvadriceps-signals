package messaging

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/platform/contracts"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

type fakePublisher struct {
	message amqp.Publishing
	key     string
	err     error
}

func (p *fakePublisher) publish(_ context.Context, _, key string, message amqp.Publishing) error {
	p.message, p.key = message, key
	return p.err
}

func TestApplicationAttemptSurvivesBrokerRedeliveryAndRequiresConfirmation(t *testing.T) {
	for _, failed := range []bool{false, true} {
		publisher := &fakePublisher{}
		if failed {
			publisher.err = errors.New("confirm lost")
		}
		conn := &Conn{log: zerolog.Nop(), parker: publisher}
		ack := &fakeAck{}
		delivery := amqp.Delivery{Acknowledger: ack, Redelivered: true, Body: []byte("payload"), MessageId: "stable-id"}
		conn.dispatch(t.Context(), ConsumerConfig{Queue: "parse.pdf"}, delivery, func(ctx context.Context, _ []byte) error {
			if contracts.FinalAttempt(ctx) {
				t.Fatal("broker redelivery exhausted application attempts")
			}
			return errors.New("transient")
		})
		if publisher.key != "parse.pdf.retry.1" ||
			messageAttempt(
				publisher.message.Headers,
			) != 1 ||
			publisher.message.MessageId != "stable-id" {
			t.Fatalf("retry metadata: %+v", publisher)
		}
		if failed && (ack.acked || !ack.requeue) {
			t.Fatal("unconfirmed transfer lost original")
		}
		if !failed && (!ack.acked || ack.nacked) {
			t.Fatal("confirmed transfer did not acknowledge original")
		}
	}
}

func TestBrokerDelayedRetryAndMandatoryRouting(t *testing.T) {
	conn := dialTest(t)
	if err := conn.DeclareTopology(); err != nil {
		t.Fatal(err)
	}
	ch, err := conn.conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	queue := "test.retry." + uuid.NewV4().String()
	if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err = declareRetries(ch, queue); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, e := conn.conn.Channel()
		if e != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		_, _ = cleanup.QueueDelete(queue, false, false, false)
		for i := range 3 {
			_, _ = cleanup.QueueDelete(retryQueue(queue, i+1), false, false, false)
		}
	})
	pub, err := conn.NewPublisher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	if err = pub.publish(t.Context(), "", queue+".missing", amqp.Publishing{Body: []byte("unroutable")}); err == nil {
		t.Fatal("unroutable message accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan time.Duration, 1)
	done := make(chan error, 1)
	go func() {
		var first time.Time
		done <- conn.Consume(ctx, ConsumerConfig{Queue: queue, Workers: 1}, func(context.Context, []byte) error {
			if first.IsZero() {
				first = time.Now()
				return errors.New("temporary failure")
			}
			finished <- time.Since(first)
			return nil
		})
	}()
	if err = pub.publish(t.Context(), "", queue, amqp.Publishing{Body: []byte("retry"), DeliveryMode: amqp.Persistent}); err != nil {
		t.Fatal(err)
	}
	select {
	case delay := <-finished:
		if delay < 4*time.Second {
			t.Fatalf("hot retry loop: %s", delay)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("delayed retry missing")
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
