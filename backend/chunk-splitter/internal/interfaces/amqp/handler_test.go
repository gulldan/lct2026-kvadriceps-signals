package amqp_test

import (
	"context"
	"testing"

	"github.com/example/chunk-splitter/internal/interfaces/amqp"
	"github.com/example/chunk-splitter/internal/platform/contracts"
)

func TestHandlerBadBody(t *testing.T) {
	if err := amqp.Handler(nil)(context.Background(), []byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatalf("Handler expected unmarshal error, got nil")
	}
}

func TestHandlerSendsUndecodableBodyForReview(t *testing.T) {
	err := amqp.Handler(nil)(context.Background(), []byte{0xff, 0xff, 0xff})
	if err == nil {
		t.Fatal("Handler expected unmarshal error, got nil")
	}
	if !contracts.IsUnrecoverable(err) {
		t.Fatalf("an undecodable body was left to be retried: %v", err)
	}
}
