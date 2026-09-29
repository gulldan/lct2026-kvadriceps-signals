package retrieval

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/vectorstore"
)

// Длинный документ обрезается, а не роняет просмотр; фрагменты идут по chunk_index, так что это стабильный префикс.
const chunkScrollLimit = 3000

// ChunkReader читает текст из payload Qdrant: он же — канонический склад извлечённого содержимого документов.
type ChunkReader struct {
	client *vectorstore.Qdrant
}

func NewChunkReader(client *vectorstore.Qdrant) *ChunkReader {
	return &ChunkReader{client: client}
}

func (r *ChunkReader) DocumentChunks(ctx context.Context, ownerID, documentID string) ([]domain.StoredChunk, error) {
	eq := map[string]string{payloadDocumentID: documentID}
	if ownerID != "" {
		eq["owner_id"] = ownerID
	}
	points, err := r.client.Scroll(ctx, eq, "chunk_index", chunkScrollLimit)
	if err != nil {
		return nil, fmt.Errorf("scroll chunks: %w", err)
	}
	chunks := make([]domain.StoredChunk, 0, len(points))
	for _, p := range points {
		chunks = append(chunks, domain.StoredChunk{
			ID:             p.ID,
			Index:          payloadInt(p.Payload, "chunk_index"),
			Text:           payloadString(p.Payload, "text"),
			IndexSnapshot:  payloadString(p.Payload, "source_index_snapshot"),
			Filename:       payloadString(p.Payload, "filename"),
			CharStart:      payloadInt(p.Payload, "char_start"),
			CharEnd:        payloadInt(p.Payload, "char_end"),
			PageStart:      payloadInt(p.Payload, "page_start"),
			PageEnd:        payloadInt(p.Payload, "page_end"),
			SectionHeading: payloadString(p.Payload, "section_heading"),
		})
	}
	slices.SortFunc(chunks, func(a, b domain.StoredChunk) int { return cmp.Compare(a.Index, b.Index) })
	return chunks, nil
}
