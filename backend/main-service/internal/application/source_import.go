package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/dbclient"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// existingSource не пишет второй объект при точном повторе импорта; другое происхождение всё равно
// упрётся в защиту неизменяемости в базе.
func (s *IngestionService) existingSource(ctx context.Context, cmd domain.UploadCommand) (*commonv1.Document, error) {
	source, err := sourcedoc.Decode([]byte(cmd.SourceMetadata))
	if err != nil {
		return nil, fmt.Errorf("invalid upload source: %w: %w", err, ErrInvalidArgument)
	}
	doc, err := s.docs.FindDocumentByHash(ctx, cmd.OwnerID, cmd.Hash)
	if errors.Is(err, dbclient.ErrNotFound) {
		return nil, dbclient.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find existing source: %w", err)
	}
	if doc.GetSourceMetadata() == "" {
		return nil, dbclient.ErrNotFound
	}
	previous, decodeErr := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
	if decodeErr != nil {
		return nil, fmt.Errorf("invalid stored source: %w", decodeErr)
	}
	a, marshalErr := json.Marshal(source)
	if marshalErr != nil {
		return nil, fmt.Errorf("encode upload source: %w", marshalErr)
	}
	b, marshalErr := json.Marshal(previous)
	if marshalErr != nil {
		return nil, fmt.Errorf("encode existing source: %w", marshalErr)
	}
	if bytes.Equal(a, b) {
		return doc, nil
	}
	return nil, dbclient.ErrNotFound
}
