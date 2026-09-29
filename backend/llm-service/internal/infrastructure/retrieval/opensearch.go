package retrieval

import (
	"context"
	"fmt"
	"strconv"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/searchstore"
)

type OpenSearchRetriever struct {
	client *searchstore.OpenSearch
}

func NewOpenSearch(client *searchstore.OpenSearch) *OpenSearchRetriever {
	return &OpenSearchRetriever{client: client}
}

func (r *OpenSearchRetriever) Retrieve(
	ctx context.Context,
	query string,
	_ []float32,
	f domain.RetrievalFilter,
	topN int,
) ([]domain.RetrievedChunk, error) {
	if query == "" || topN <= 0 {
		return nil, nil
	}
	hits, err := r.client.SearchFiltered(ctx, query, f.OwnerID, topN, f.ScopeDocumentIDs, f.ExcludeDocumentIDs)
	if err != nil {
		return nil, fmt.Errorf("opensearch search: %w", err)
	}
	chunks := make([]domain.RetrievedChunk, 0, len(hits))
	for _, h := range hits {
		chunks = append(chunks, domain.RetrievedChunk{
			ID:            h.ID,
			Text:          h.Text,
			DocumentID:    h.DocumentID,
			Filename:      h.Filename,
			IndexSnapshot: h.Metadata["source_index_snapshot"],
			CharStart:     metadataNumber(h.Metadata, "char_start"),
			CharEnd:       metadataNumber(h.Metadata, "char_end"),
			PageStart:     metadataNumber(h.Metadata, "page_start"),
			PageEnd:       metadataNumber(h.Metadata, "page_end"),
			// chunk_index в поисковом индексе нет — его добирает слияние из payload Qdrant.
			Score: h.Score,
		})
	}
	return chunks, nil
}

func metadataNumber(metadata map[string]string, key string) int {
	value, _ := strconv.Atoi(metadata[key])
	return value
}
