package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/indexdoc"
)

type SourceIndexReader interface {
	ReadIndexes(context.Context, indexdoc.Read) (indexdoc.States, error)
}

// EnableSourceReads делает проверку доступа обязательной; requireReady=false лишь пускает старые фрагменты без снимка.
func (s *RAGService) EnableSourceReads(reader SourceIndexReader, requireReady bool) {
	s.sourceIndexes, s.requireSourceReady = reader, requireReady
}

// Answer сверяет эпоху доступа до кэша и после ответа: отзыв доступа во время генерации не отдаст её текст.
func (s *RAGService) Answer(ctx context.Context, q domain.Query) (domain.Result, error) {
	ctx = context.WithValue(ctx, cacheRevisionKey{}, s.currentCacheRevision(ctx))
	q.AccessRevision = ""
	guard := &sourceReadGuard{ids: map[string]bool{}}
	if s.sourceIndexes != nil {
		ctx = context.WithValue(ctx, sourceGuardKey{}, guard)
		revision, err := s.accessRevision(ctx, q.OwnerID)
		if err != nil {
			return domain.Result{}, err
		}
		q.AccessRevision = revision
	}
	result, err := s.answer(ctx, q)
	if err != nil || s.sourceIndexes == nil {
		return result, err
	}
	revision, err := s.accessRevision(ctx, q.OwnerID)
	if err != nil {
		return domain.Result{}, err
	}
	if revision != q.AccessRevision && (result.Cached || !s.guardStillAllowed(ctx, q.OwnerID, guard)) {
		return domain.Result{}, errors.New("source access or index changed; retry the query")
	}
	return result, nil
}

func (s *RAGService) accessRevision(ctx context.Context, reader string) (string, error) {
	state, err := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{ReaderID: reader, SharedCorpus: s.sharedCorpus})
	if err != nil || state.Epoch < 1 {
		return "", errors.New("source access is unavailable")
	}
	return reader + "/" + strconv.FormatInt(state.Epoch, 10), nil
}

func (s *RAGService) sourceStates(ctx context.Context, reader string, ids []string) (map[string]indexdoc.State, error) {
	states := make(map[string]indexdoc.State, len(ids))
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for batch := range slices.Chunk(ids, 256) {
		result, err := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
			ReaderID: reader, SharedCorpus: s.sharedCorpus, DocumentIDs: batch,
		})
		if err != nil {
			return nil, fmt.Errorf("read source access: %w", err)
		}
		for _, state := range result.Documents {
			states[state.DocumentID] = state
			if guard, ok := ctx.Value(sourceGuardKey{}).(*sourceReadGuard); ok && state.Allowed {
				guard.add(state.DocumentID)
			}
		}
	}
	return states, nil
}

type sourceGuardKey struct{}

// Помним все допущенные документы, включая прочитанные агентом:
// смена эпохи из-за чужой индексации не должна отменять ответ.
type sourceReadGuard struct {
	mu  sync.Mutex
	ids map[string]bool
}

func (g *sourceReadGuard) add(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ids[id] = true
}

func (s *RAGService) guardStillAllowed(ctx context.Context, reader string, guard *sourceReadGuard) bool {
	guard.mu.Lock()
	ids := make([]string, 0, len(guard.ids))
	for id := range guard.ids {
		ids = append(ids, id)
	}
	guard.mu.Unlock()
	states, err := s.sourceStates(ctx, reader, ids)
	if err != nil {
		return false
	}
	for _, id := range ids {
		if !states[id].Allowed {
			return false
		}
	}
	return true
}

func (s *RAGService) generationAllowed(state indexdoc.State, snapshot string) bool {
	if !state.Allowed {
		return false
	}
	if state.Active != nil {
		return snapshot == state.Active.SnapshotID
	}
	return !s.requireSourceReady && snapshot == ""
}

// Сводки разворачиваем до реранка: в тексте сводки бывают куски членов, которых нет в её списке ссылок.
func (s *RAGService) allowedHits(
	ctx context.Context, reader string, hits []domain.RetrievedChunk, tr *domain.Trace,
) []domain.RetrievedChunk {
	if s.sourceIndexes == nil {
		return hits
	}
	leaves, _ := expandSummaries(hits)
	ids := make([]string, 0, len(leaves))
	for _, hit := range leaves {
		if hit.DocumentID != "" && hit.NodeType != nodeTypeRaptorSummary {
			ids = append(ids, hit.DocumentID)
		}
	}
	states, err := s.sourceStates(ctx, reader, ids)
	if err != nil {
		tr.MarkDegraded("source_access_unavailable")
		return nil
	}
	out := make([]domain.RetrievedChunk, 0, len(leaves))
	for _, hit := range leaves {
		if hit.NodeType == nodeTypeRaptorSummary || !s.generationAllowed(states[hit.DocumentID], hit.IndexSnapshot) {
			continue
		}
		// id сводки раскрыл бы приватную группировку смешанных документов.
		hit.RaptorSummaryID, hit.Members, hit.MemberIDs = "", nil, nil
		out = append(out, hit)
	}
	return out
}

func (s *RAGService) Chunks(ctx context.Context, reader, documentID string) ([]domain.StoredChunk, error) {
	scope := reader
	if s.sharedCorpus {
		scope = ""
	}
	if s.sourceIndexes == nil {
		return s.chunks.DocumentChunks(ctx, scope, documentID)
	}
	states, err := s.sourceStates(ctx, reader, []string{documentID})
	if err != nil {
		return nil, err
	}
	state := states[documentID]
	if !state.Allowed {
		return nil, indexdoc.ErrNotFound
	}
	stored, err := s.chunks.DocumentChunks(ctx, state.OwnerID, documentID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.StoredChunk, 0, len(stored))
	for _, chunk := range stored {
		if s.generationAllowed(state, chunk.IndexSnapshot) {
			out = append(out, chunk)
		}
	}
	return out, nil
}
