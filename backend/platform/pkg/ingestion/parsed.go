package ingestion

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/example/rag-mvp/pkg/contracts"
	"github.com/example/rag-mvp/pkg/logger"

	commonv1 "github.com/example/rag-mvp/pkg/genproto/common/v1"
)

// InlineTextMax ограничивает тело сообщения: крупный текст передаётся ссылкой на объект.
const InlineTextMax = 4 << 20

type ObjectWriter interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

// PublishParsed публикует ссылку лишь после сохранения текста. Ключ зависит от содержимого,
// поэтому повторная доставка не меняет текст, на который уже ссылаются цитаты.
func PublishParsed(ctx context.Context, store ObjectWriter, pub Publisher, parsed *commonv1.DocumentParsed) error {
	text := parsed.GetText()
	if len(text) > InlineTextMax {
		key := contracts.ParsedObjectKey(parsed.GetDocumentId(), text)
		if err := store.Put(ctx, key, strings.NewReader(text), int64(len(text)), "text/plain; charset=utf-8"); err != nil {
			return fmt.Errorf("store parsed text: %w", err)
		}
		parsed.Text, parsed.TextObjectKey = "", key
		logger.From(ctx).Info().Str("document_id", parsed.GetDocumentId()).Str("source", parsed.GetSource()).
			Int("chars", len(text)).Str("text_object_key", key).Msg("text offloaded to object store")
	}
	if err := pub.PublishProto(ctx, contracts.ExchangeIngestion, contracts.RouteChunk, parsed); err != nil {
		return fmt.Errorf("publish parsed: %w", err)
	}
	return nil
}
