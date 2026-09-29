package retrieval

import (
	"context"
	"fmt"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/vectorstore"
)

type QdrantRetriever struct {
	client *vectorstore.Qdrant
}

const payloadDocumentID = "document_id"

func NewQdrant(client *vectorstore.Qdrant) *QdrantRetriever {
	return &QdrantRetriever{client: client}
}

func (r *QdrantRetriever) Retrieve(
	ctx context.Context,
	_ string,
	embedding []float32,
	f domain.RetrievalFilter,
	topN int,
) ([]domain.RetrievedChunk, error) {
	if len(embedding) == 0 || topN <= 0 {
		return nil, nil
	}
	filter := vectorstore.SearchFilter{}
	if f.OwnerID != "" {
		filter.Eq = map[string]string{"owner_id": f.OwnerID}
	}
	if len(f.ScopeDocumentIDs) > 0 {
		filter.AnyOf = map[string][]string{payloadDocumentID: f.ScopeDocumentIDs}
	}
	if len(f.ExcludeDocumentIDs) > 0 {
		filter.NoneOf = map[string][]string{payloadDocumentID: f.ExcludeDocumentIDs}
	}
	hits, err := r.client.SearchFiltered(ctx, embedding, topN, filter)
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}
	chunks := make([]domain.RetrievedChunk, 0, len(hits))
	for _, h := range hits {
		chunks = append(chunks, chunkFromPayload(h.ID, float64(h.Score), h.Payload))
	}
	return chunks, nil
}

func chunkFromPayload(id string, score float64, payload map[string]any) domain.RetrievedChunk {
	return domain.RetrievedChunk{
		ID:             id,
		Text:           payloadString(payload, "text"),
		DocumentID:     payloadString(payload, payloadDocumentID),
		IndexSnapshot:  payloadString(payload, "source_index_snapshot"),
		Filename:       payloadString(payload, "filename"),
		ChunkIndex:     payloadInt(payload, "chunk_index"),
		Score:          score,
		CharStart:      payloadInt(payload, "char_start"),
		CharEnd:        payloadInt(payload, "char_end"),
		PageStart:      payloadInt(payload, "page_start"),
		PageEnd:        payloadInt(payload, "page_end"),
		SectionHeading: payloadString(payload, "section_heading"),
		NodeType:       payloadString(payload, "node_type"),
		NodeLevel:      payloadInt(payload, "node_level"),
		MemberIDs:      payloadStrings(payload, "member_ids"),
		Members:        payloadMembers(payload, "members"),
	}
}

func payloadStrings(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func payloadMembers(payload map[string]any, key string) []domain.RetrievedChunk {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]domain.RetrievedChunk, 0, len(raw))
	for _, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, chunkFromPayload(payloadString(m, "id"), 0, m))
	}
	return out
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func payloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch v := payload[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
