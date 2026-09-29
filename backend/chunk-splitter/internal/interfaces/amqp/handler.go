package amqp

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/example/chunk-splitter/internal/application"

	"github.com/example/chunk-splitter/internal/platform/contracts"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
)

// Handler отправляет недекодируемое тело и нарушение схемы на разбор человеку, а не в повтор: повтор упадёт
// так же, а продюсер с новым контрактом — проблема выкладки, которую надо увидеть.
func Handler(ix *application.Indexer) func(ctx context.Context, body []byte) error {
	return func(ctx context.Context, body []byte) error {
		var evt commonv1.DocumentParsed
		if err := proto.Unmarshal(body, &evt); err != nil {
			return contracts.Unrecoverable(fmt.Errorf("unmarshal DocumentParsed: %w", err))
		}
		err := ix.Process(ctx, &evt)
		if errors.Is(err, indexdoc.ErrInvalid) {
			return contracts.Unrecoverable(err)
		}
		return err
	}
}
