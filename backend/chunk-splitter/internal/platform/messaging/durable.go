package messaging

import (
	"context"

	"github.com/example/chunk-splitter/internal/platform/contracts"
	"google.golang.org/protobuf/proto"
)

type Outbox interface {
	PublishIngestion(context.Context, string, proto.Message) error
}

// Durable отделяет обязательную передачу обработки от уведомлений интерфейса.
type Durable struct {
	Events *Publisher
	Outbox Outbox
}

// PublishProto сохраняет обязательное событие до подтверждения предыдущей стадии.
func (p Durable) PublishProto(ctx context.Context, exchange, route string, event proto.Message) error {
	if exchange == contracts.ExchangeIngestion {
		return p.Outbox.PublishIngestion(ctx, route, event)
	}
	return p.Events.PublishProto(ctx, exchange, route, event)
}
