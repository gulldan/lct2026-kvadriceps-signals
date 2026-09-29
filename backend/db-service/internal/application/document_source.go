package application

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/sourcedoc"
)

func validateInitialSource(raw []byte, contentHash string) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	source, err := sourcedoc.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, domain.ErrInvalidArgument)
	}
	if source.HashAlgorithm == "blake3" && source.ContentHash != contentHash {
		return nil, fmt.Errorf("source hash differs from upload: %w", domain.ErrInvalidArgument)
	}
	metadata, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("encode source: %w", err)
	}
	return metadata, nil
}

// SetDocumentSource проверяет типизированное происхождение и не доверяет docinfo от разборщика.
func (s *Service) SetDocumentSource(ctx context.Context, id, ownerID string, raw []byte) error {
	if id == "" || ownerID == "" {
		return fmt.Errorf("id and owner_id required: %w", domain.ErrInvalidArgument)
	}
	source, err := sourcedoc.Decode(raw)
	if err != nil {
		return fmt.Errorf("%w: %w", err, domain.ErrInvalidArgument)
	}
	d, err := s.docs.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.OwnerID != ownerID {
		return domain.ErrNotFound
	}
	if d.Kind == "hypotheses" && source.Kind != "hypothesis" {
		return fmt.Errorf("generated hypothesis cannot be a scientific source: %w", domain.ErrInvalidArgument)
	}
	if source.HashAlgorithm == "blake3" && d.ContentHash != "" && d.ContentHash != source.ContentHash {
		return fmt.Errorf("source hash differs from document bytes: %w", domain.ErrConflict)
	}
	metadata, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("marshal source: %w", err)
	}
	return s.docs.SetSource(ctx, id, ownerID, metadata)
}
