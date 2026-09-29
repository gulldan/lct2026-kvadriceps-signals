package application

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/example/llm-service/internal/domain"
)

const (
	evidenceNeighborRadius = 2
	evidenceNeighborDocs   = 3
	evidenceNeighborLimit  = 24
)

// Реранкер часто выбирает начало абзаца без его вывода. Соседи возвращают результат и условия
// эксперимента, сохраняя отдельные ссылки и проверку доступа к той же версии документа.
func (s *RAGService) withEvidenceNeighbors(
	ctx context.Context, reader string, ranked []domain.RetrievedChunk,
) []domain.RetrievedChunk {
	if s.chunks == nil || len(ranked) == 0 || len(ranked) >= evidenceNeighborLimit {
		return ranked
	}
	documents := s.readNeighborDocuments(ctx, reader, ranked)
	out := slices.Clone(ranked)
	seen := make(map[string]bool, len(ranked))
	for _, hit := range ranked {
		seen[hit.ID] = true
	}
	for _, hit := range ranked {
		if s.rerankerActive && hit.Score < s.scoreFloor {
			continue
		}
		for _, neighbor := range evidenceWindow(hit, documents[hit.DocumentID]) {
			if seen[neighbor.ID] {
				continue
			}
			seen[neighbor.ID] = true
			out = append(out, neighbor)
			if len(out) >= evidenceNeighborLimit {
				return out
			}
		}
	}
	return out
}

func (s *RAGService) readNeighborDocuments(
	ctx context.Context, reader string, ranked []domain.RetrievedChunk,
) map[string][]domain.StoredChunk {
	docIDs := make([]string, 0, evidenceNeighborDocs)
	for _, hit := range ranked {
		if hit.DocumentID == "" || (s.rerankerActive && hit.Score < s.scoreFloor) || slices.Contains(docIDs, hit.DocumentID) {
			continue
		}
		docIDs = append(docIDs, hit.DocumentID)
		if len(docIDs) == evidenceNeighborDocs {
			break
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	documents := make([][]domain.StoredChunk, len(docIDs))
	var wg sync.WaitGroup
	for i, id := range docIDs {
		wg.Go(func() {
			// При сбое дополнительного чтения исходная выдача остаётся доступной.
			documents[i], _ = s.Chunks(readCtx, reader, id)
		})
	}
	wg.Wait()
	out := make(map[string][]domain.StoredChunk, len(docIDs))
	for i, id := range docIDs {
		out[id] = documents[i]
	}
	return out
}

func evidenceWindow(hit domain.RetrievedChunk, chunks []domain.StoredChunk) []domain.RetrievedChunk {
	anchor := slices.IndexFunc(chunks, func(c domain.StoredChunk) bool {
		return c.ID == hit.ID && c.IndexSnapshot == hit.IndexSnapshot
	})
	if anchor < 0 {
		return nil
	}
	out := make([]domain.RetrievedChunk, 0, 2*evidenceNeighborRadius+1)
	for _, chunk := range chunks {
		if chunk.ID == "" || chunk.Text == "" || chunk.IndexSnapshot != hit.IndexSnapshot ||
			chunk.Index < chunks[anchor].Index-evidenceNeighborRadius || chunk.Index > chunks[anchor].Index+evidenceNeighborRadius {
			continue
		}
		out = append(out, domain.RetrievedChunk{
			ID: chunk.ID, DocumentID: hit.DocumentID, Filename: cmp.Or(chunk.Filename, hit.Filename),
			Text: chunk.Text, ChunkIndex: chunk.Index, IndexSnapshot: chunk.IndexSnapshot,
			CharStart: chunk.CharStart, CharEnd: chunk.CharEnd, PageStart: chunk.PageStart, PageEnd: chunk.PageEnd,
			SectionHeading: chunk.SectionHeading, Origin: "neighbor",
		})
	}
	return out
}
