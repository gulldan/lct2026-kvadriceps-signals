package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// SourceEvidence проверяет текущие права доступа; snapshotID можно опустить только при первом открытии
// источника.
func (s *IngestionService) SourceEvidence(ctx context.Context, reader, id, snapshotID string) (*indexdoc.Snapshot, error) {
	manifest, err := s.sourceEvidenceManifest(ctx, reader, id, snapshotID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.readSourceSnapshot(ctx, manifest)
	if err != nil {
		return nil, err
	}
	if err = s.verifySourceAnchors(ctx, snapshot, manifest); err != nil {
		return nil, err
	}
	// Отзыв доступа, пока грузились артефакты, тоже должен скрыть ответ.
	if _, err = s.sourceEvidenceManifest(ctx, reader, id, manifest.SnapshotID); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *IngestionService) sourceEvidenceManifest(ctx context.Context, reader, id, snapshotID string,
) (*indexdoc.Manifest, error) {
	if s.sourceIndexes == nil {
		return nil, ErrNotFound
	}
	states, err := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
		ReaderID: reader, SharedCorpus: s.sharedCorpus, DocumentIDs: []string{id}, SnapshotID: snapshotID,
	})
	if err != nil {
		return nil, err
	}
	if len(states.Documents) != 1 {
		return nil, ErrNotFound
	}
	state := states.Documents[0]
	if !state.Allowed || state.DocumentID != id || state.Active == nil || state.Active.DocumentID != id ||
		(snapshotID != "" && state.Active.SnapshotID != snapshotID) {
		return nil, ErrNotFound
	}
	return state.Active, nil
}

func (s *IngestionService) verifySourceAnchors(ctx context.Context, snapshot *indexdoc.Snapshot,
	manifest *indexdoc.Manifest) error {
	stream, err := s.store.Get(ctx, manifest.Text.Key)
	if err != nil {
		return fmt.Errorf("load source text: %w", err)
	}
	defer func() { _ = stream.Close() }()
	raw, err := io.ReadAll(io.LimitReader(stream, indexdoc.MaxTextBytes+1))
	if err != nil {
		return fmt.Errorf("read source text: %w", err)
	}
	if len(raw) > indexdoc.MaxTextBytes || !utf8.Valid(raw) || indexdoc.Hash(raw) != snapshot.TextHash {
		return errors.New("source text artifact hash mismatch")
	}
	text := []rune(string(raw))
	// Одно правило для всех локаторов платформы: текст по смещениям и есть цитата, иначе локатор отклоняется.
	for _, chunk := range snapshot.Chunks {
		locator := indexdoc.Locator{Section: chunk.Section, StartChar: chunk.CharStart, EndChar: chunk.CharEnd}
		if locator.Verify(text, chunk.Text) != nil {
			return errors.New("source fragment does not match its saved text locator")
		}
	}
	if err = sourcedoc.ValidateSections(snapshot.Sections, len(text)); err != nil {
		return fmt.Errorf("source sections do not address the stored text: %w", err)
	}
	if err = sourcedoc.ValidateAnchors(snapshot.Anchors, len(text)); err != nil {
		return fmt.Errorf("source anchors do not address the stored text: %w", err)
	}
	return nil
}
