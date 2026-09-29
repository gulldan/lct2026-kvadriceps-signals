package vectorindex

import (
	"context"

	"github.com/example/chunk-splitter/internal/application"
	"github.com/example/chunk-splitter/internal/platform/vectorstore"
)

type Adapter struct {
	client *vectorstore.Qdrant
}

func New(client *vectorstore.Qdrant) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) Upsert(ctx context.Context, points []application.VectorPoint) error {
	out := make([]vectorstore.Point, len(points))
	for i, p := range points {
		payload := map[string]any{
			"document_id":     p.DocumentID,
			"owner_id":        p.OwnerID,
			"filename":        p.Filename,
			"text":            p.Text,
			"chunk_index":     p.ChunkIndex,
			"char_start":      p.CharStart,
			"char_end":        p.CharEnd,
			"page_start":      p.PageStart,
			"page_end":        p.PageEnd,
			"section_heading": p.SectionHeading,
			// raptor-worker пишет сводки в ту же коллекцию, и по node_type поиск сворачивает дерево
			// и раскрывает сводку до листьев.
			"node_type":  p.NodeType,
			"node_level": p.NodeLevel,
			"member_ids": memberIDs(p.MemberIDs),
		}
		for key, value := range p.Metadata {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
		out[i] = vectorstore.Point{
			ID:      p.ID,
			Vector:  p.Vector,
			Payload: payload,
		}
	}
	return a.client.Upsert(ctx, out)
}

// Пустой, но не nil срез: у листа member_ids: [], а не null — как у сводок raptor-worker.
func memberIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func (a *Adapter) Confirm(ctx context.Context, owner, docID, snapshot string, ids []string, dimension int) error {
	return a.client.ConfirmIndex(ctx, owner, docID, snapshot, ids, dimension)
}

func (a *Adapter) DeleteIDs(ctx context.Context, owner, docID string, ids []string) error {
	return a.client.DeleteChunkIDs(ctx, owner, docID, ids)
}
