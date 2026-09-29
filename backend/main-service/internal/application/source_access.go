package application

import (
	"context"
	"errors"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"google.golang.org/protobuf/proto"
)

// SetSourceAccess: отозвать или вернуть исходную видимость может только владелец — права администратора
// владение не заменяют.
func (s *IngestionService) SetSourceAccess(ctx context.Context, in indexdoc.AccessChange) error {
	doc, err := s.docs.GetDocument(ctx, in.DocumentID)
	if err != nil {
		return err
	}
	if doc.GetOwnerId() != in.OwnerID || doc.GetSourceMetadata() == "" {
		return ErrNotFound
	}
	writer, ok := s.sourceIndexes.(interface {
		SetSourceAccess(context.Context, indexdoc.AccessChange) error
	})
	if !ok {
		return errors.New("source access service unavailable")
	}
	return writer.SetSourceAccess(ctx, in)
}

// DocumentSnapshotChunks отдаёт процитированное поколение под текущими правами и не подменяет его
// сегодняшним текстом.
func (s *IngestionService) DocumentSnapshotChunks(ctx context.Context, reader, id, snapshotID string,
) (*commonv1.Document, []*llmv1.DocumentChunk, error) {
	if s.sourceIndexes == nil {
		return nil, nil, ErrNotFound
	}
	states, err := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
		ReaderID: reader, SharedCorpus: s.sharedCorpus, DocumentIDs: []string{id}, SnapshotID: snapshotID,
	})
	if err != nil {
		return nil, nil, err
	}
	if len(states.Documents) != 1 || !states.Documents[0].Allowed || states.Documents[0].Active == nil {
		return nil, nil, ErrNotFound
	}
	manifest := states.Documents[0].Active
	if manifest.DocumentID != id || manifest.SnapshotID != snapshotID {
		return nil, nil, ErrNotFound
	}
	doc, err := s.docs.GetDocument(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	chunks, err := s.sourceChunks(ctx, manifest)
	if err != nil {
		return nil, nil, err
	}
	doc = proto.CloneOf(doc)
	doc.ChunkCount = int32(len(chunks))
	return doc, chunks, nil
}
