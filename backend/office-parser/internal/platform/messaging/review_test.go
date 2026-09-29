package messaging

import (
	"strconv"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/example/office-parser/internal/platform/contracts"
)

func TestBrokerReviewReadsDistinctMessagesWithoutConsuming(t *testing.T) {
	conn := dialTest(t)
	if err := conn.DeclareTopology(); err != nil {
		t.Fatal(err)
	}
	pub, err := conn.NewPublisher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	prefix := "review-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	for i := range 2 {
		if err = pub.publish(t.Context(), contracts.ExchangeDLX, "", amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			Headers:      amqp.Table{contracts.HeaderFailureReason: prefix + strconv.Itoa(i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		messages, err := conn.PeekDead(100)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, message := range messages {
			if strings.HasPrefix(message.Reason, prefix) {
				if seen[message.Reason] {
					t.Fatal("review returned the same delivery more than once")
				}
				seen[message.Reason] = true
			}
		}
		if len(seen) != 2 {
			t.Fatalf("review read %d distinct messages, want 2 on every inspection", len(seen))
		}
	}
}

func TestBrokerParkingWaitsForBrokerAcceptance(t *testing.T) {
	conn := dialTest(t)
	var err error
	conn.parker, err = conn.NewPublisher()
	if err != nil {
		t.Fatal(err)
	}
	// Сама публикация проходит до того, как брокер отвергнет несуществующий обменник: только ожидание
	// подтверждения не даёт подтвердить входное сообщение.
	if err = conn.parker.publish(t.Context(), "missing-"+strconv.FormatInt(time.Now().UnixNano(), 10), "",
		amqp.Publishing{Body: []byte("must survive")}); err == nil {
		t.Fatal("parking reported success without the broker accepting the message")
	}
}
