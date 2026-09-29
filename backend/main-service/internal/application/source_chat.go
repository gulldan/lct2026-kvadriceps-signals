package application

import (
	"context"
	"errors"
	"slices"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
)

// EnableSourceReads перепроверяет цитаты перед показом или продолжением диалога: после отзыва доступа
// сохранённый текст остаётся в базе, но недоступен.
func (s *ChatService) EnableSourceReads(reader SourceIndexReader, shared bool) {
	s.sourceIndexes, s.sharedCorpus = reader, shared
}

func (s *ChatService) checkSourceHistory(ctx context.Context, owner string, messages []*commonv1.Message) error {
	if s.sourceIndexes == nil {
		return nil
	}
	ids := []string{}
	for _, message := range messages {
		for _, source := range message.GetSources() {
			if source.GetDocumentId() != "" {
				ids = append(ids, source.GetDocumentId())
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for batch := range slices.Chunk(ids, 256) {
		states, err := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
			ReaderID: owner, SharedCorpus: s.sharedCorpus, DocumentIDs: batch, AccessOnly: true,
		})
		if err != nil {
			return errors.New("source access unavailable")
		}
		allowed := map[string]bool{}
		for _, state := range states.Documents {
			allowed[state.DocumentID] = state.Allowed
		}
		for _, id := range batch {
			if !allowed[id] {
				return ErrNotFound
			}
		}
	}
	return nil
}
