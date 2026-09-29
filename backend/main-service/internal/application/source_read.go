package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// SourceIndexReader заново проверяет доступ при каждом чтении снимка.
type SourceIndexReader interface {
	ReadIndexes(context.Context, indexdoc.Read) (indexdoc.States, error)
}

// EnableSourceReads: административный обход действует только для старых документов без происхождения.
func (s *IngestionService) EnableSourceReads(reader SourceIndexReader, sharedCorpus, requireReady bool) {
	s.sourceIndexes, s.sharedCorpus, s.requireSourceReady = reader, sharedCorpus, requireReady
}

func (s *IngestionService) sourceAccess(ctx context.Context, doc *commonv1.Document,
	requester string, legacyPrivileged bool) (indexdoc.State, error) {
	if doc.GetSourceMetadata() == "" {
		if !legacyPrivileged && doc.GetOwnerId() != requester {
			return indexdoc.State{}, ErrForbidden
		}
		if s.sourceIndexes == nil {
			return indexdoc.State{Allowed: true}, nil
		}
		return s.readDocumentIndex(ctx, doc, doc.GetOwnerId())
	}
	source, err := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
	if err != nil {
		return indexdoc.State{}, fmt.Errorf("read source access policy: %w", err)
	}
	if !indexdoc.CanRead(requester, doc.GetOwnerId(), source, s.sharedCorpus, false) {
		return indexdoc.State{}, ErrNotFound
	}
	if s.sourceIndexes == nil {
		return indexdoc.State{Allowed: true, Source: source}, nil
	} // тестовый адаптер без долговечного отзыва доступа
	return s.readDocumentIndex(ctx, doc, requester)
}

func (s *IngestionService) readDocumentIndex(
	ctx context.Context,
	doc *commonv1.Document,
	requester string,
) (indexdoc.State, error) {
	states, err := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
		ReaderID: requester, SharedCorpus: s.sharedCorpus, DocumentIDs: []string{doc.GetId()},
	})
	if err != nil {
		return indexdoc.State{}, fmt.Errorf("verify source access: %w", err)
	}
	if len(states.Documents) != 1 || states.Documents[0].DocumentID != doc.GetId() || !states.Documents[0].Allowed {
		return indexdoc.State{}, ErrNotFound
	}
	return states.Documents[0], nil
}

func (s *IngestionService) sourceChunks(ctx context.Context, manifest *indexdoc.Manifest) ([]*llmv1.DocumentChunk, error) {
	snapshot, err := s.readSourceSnapshot(ctx, manifest)
	if err != nil {
		return nil, err
	}
	chunks := make([]*llmv1.DocumentChunk, len(snapshot.Chunks))
	for i, chunk := range snapshot.Chunks {
		chunks[i] = &llmv1.DocumentChunk{Id: chunk.ID, Index: int32(i), Text: chunk.Text}
	}
	return chunks, nil
}

func (s *IngestionService) readSourceSnapshot(ctx context.Context, manifest *indexdoc.Manifest) (*indexdoc.Snapshot, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("read source index manifest: %w", err)
	}
	stream, err := s.store.Get(ctx, manifest.Data.Key)
	if err != nil {
		return nil, fmt.Errorf("read immutable source index: %w", err)
	}
	defer func() { _ = stream.Close() }()
	raw, err := io.ReadAll(io.LimitReader(stream, indexdoc.MaxSnapshotBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read source index bytes: %w", err)
	}
	if len(raw) > indexdoc.MaxSnapshotBytes || indexdoc.Hash(raw) != manifest.Data.SHA256 {
		return nil, errors.New("source index artifact hash mismatch")
	}
	var snapshot indexdoc.Snapshot
	if err = json.Unmarshal(raw, &snapshot, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("decode immutable source chunks: %w", err)
	}
	if snapshot.Version != 1 || snapshot.DocumentID != manifest.DocumentID || snapshot.OwnerID != manifest.OwnerID ||
		snapshot.SnapshotID != manifest.SnapshotID || len(snapshot.Chunks) != manifest.ChunkCount ||
		snapshot.TextHash != manifest.Text.SHA256 || snapshotRawHash(snapshot) != manifest.RawHash ||
		snapshotRawRevision(snapshot) != manifest.SourceRevision || snapshot.Model != manifest.Model ||
		snapshot.ParserRevision != manifest.ParserRevision || snapshot.PipelineRevision != manifest.PipelineRevision {
		return nil, errors.New("source chunks differ from the published manifest")
	}
	for i, chunk := range snapshot.Chunks {
		if chunk.Index != i || chunk.ID == "" {
			return nil, errors.New("invalid immutable chunk identity")
		}
	}
	return &snapshot, nil
}

func snapshotRawHash(s indexdoc.Snapshot) string {
	if s.RawHash != "" {
		return s.RawHash
	}
	return s.Source.ContentHash
}
func snapshotRawRevision(s indexdoc.Snapshot) string {
	if s.RawRevision != "" {
		return s.RawRevision
	}
	return s.Source.Revision
}
