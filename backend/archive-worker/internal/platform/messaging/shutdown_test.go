package messaging

import (
	"context"
	"errors"
	"github.com/example/archive-worker/internal/platform/contracts"
	"testing"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

func TestShutdownRequeuesRedeliveredFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ack := &fakeAck{}
	delivery := amqp.Delivery{Acknowledger: ack, Redelivered: true}
	conn := &Conn{log: zerolog.Nop()}
	conn.dispatch(ctx, ConsumerConfig{}, delivery, func(context.Context, []byte) error {
		cancel()
		return errors.New("store request interrupted by shutdown")
	})
	if !ack.nacked || !ack.requeue {
		t.Fatal("shutdown must preserve a delivery even after a previous retry")
	}
}

func TestShutdownDoesNotStartBufferedDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ack := &fakeAck{}
	conn := &Conn{log: zerolog.Nop()}
	conn.dispatch(ctx, ConsumerConfig{}, amqp.Delivery{Acknowledger: ack, Redelivered: true},
		func(context.Context, []byte) error {
			t.Fatal("stopping consumer started another buffered delivery")
			return nil
		})
	if !ack.nacked || !ack.requeue {
		t.Fatal("buffered delivery must return to its queue")
	}
}

func TestBrokerCancelPreservesPrefetchedDeliveries(t *testing.T) {
	conn := dialTest(t)
	channel, err := conn.conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = channel.Close() }()
	queue := "test.shutdown." + uuid.NewV4().String()
	if _, err = channel.QueueDeclare(queue, false, false, true, false, nil); err != nil {
		t.Fatal(err)
	}
	// Самоудаление отключено до очистки, чтобы отмена не стёрла поставленную работу.
	defer func() { _, _ = channel.QueueDelete(queue, false, false, false) }()
	for range 20 {
		if err = channel.PublishWithContext(t.Context(), "", queue, false, false,
			amqp.Publishing{Body: []byte("restart proof")}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- conn.Consume(ctx, ConsumerConfig{Queue: queue, Workers: 1, Prefetch: 20},
			func(ctx context.Context, _ []byte) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			})
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not start")
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not stop after cancellation")
	}
	state, err := channel.QueueDeclarePassive(queue, false, false, true, false, nil)
	if err != nil || state.Messages != 20 || state.Consumers != 0 {
		t.Fatalf("after cancellation: messages=%d consumers=%d error=%v", state.Messages, state.Consumers, err)
	}
}

func TestDurabilityFailurePreservesLastAttempt(t *testing.T) {
	ack := &fakeAck{}
	delivery := amqp.Delivery{
		Acknowledger: ack,
		Redelivered:  true,
		Headers: amqp.Table{
			attemptHeader: int32(
				contracts.MaxAttempts - 1,
			),
		},
	}
	conn := &Conn{log: zerolog.Nop()}
	conn.dispatch(t.Context(), ConsumerConfig{}, delivery, func(context.Context, []byte) error { return contracts.ErrDurability })
	if !ack.nacked || !ack.requeue || ack.acked {
		t.Fatal("durability failure must preserve original delivery at last attempt")
	}
}
