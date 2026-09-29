package application

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/example/main-service/internal/platform/indexdoc"
)

// SourceOriginal сверяет исходный артефакт выбранного поколения до отдачи байтов и не подменяет его
// текущим ключом объекта документа.
func (s *IngestionService) SourceOriginal(ctx context.Context, reader, id, snapshotID string,
) ([]byte, *indexdoc.Snapshot, error) {
	if snapshotID == "" {
		return nil, nil, ErrInvalidArgument
	}
	manifest, err := s.sourceEvidenceManifest(ctx, reader, id, snapshotID)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := s.readSourceSnapshot(ctx, manifest)
	if err != nil {
		return nil, nil, err
	}
	stream, err := s.store.Get(ctx, manifest.RawKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read saved original: %w", err)
	}
	defer func() { _ = stream.Close() }()
	// Буфер ограничен: хеш и доступ проверяются до начала ответа, даже если хранилище испорчено или подменено.
	const maxOriginalBytes = 64 << 20
	raw, err := io.ReadAll(io.LimitReader(stream, maxOriginalBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read original bytes: %w", err)
	}
	if len(raw) > maxOriginalBytes || indexdoc.Hash(raw) != manifest.RawHash {
		return nil, nil, errors.New("saved original hash mismatch or size limit exceeded")
	}
	if _, err = s.sourceEvidenceManifest(ctx, reader, id, snapshotID); err != nil {
		return nil, nil, err
	}
	return raw, snapshot, nil
}
