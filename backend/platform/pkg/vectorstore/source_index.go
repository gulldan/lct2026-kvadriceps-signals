package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

func (q *Qdrant) ConfirmIndex(ctx context.Context, owner, docID, snapshot string, ids []string, dim int) error {
	if owner == "" || docID == "" || snapshot == "" || len(ids) < 1 || len(ids) > 256 || dim < 1 {
		return errors.New("invalid source index confirmation")
	}
	var out struct {
		Result []Point `json:"result"`
	}
	body := map[string]any{"ids": ids, qWithPayload: true, "with_vector": true}
	if err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points", body, &out); err != nil {
		return err
	}
	expected := make(map[string]bool, len(ids))
	for _, id := range ids {
		expected[id] = true
	}
	for _, point := range out.Result {
		if !expected[point.ID] || point.Payload[qOwnerID] != owner || point.Payload["document_id"] != docID ||
			point.Payload["source_index_snapshot"] != snapshot || len(point.Vector) != dim {
			return errors.New("source vector projection differs from its manifest")
		}
		delete(expected, point.ID)
	}
	if len(expected) != 0 {
		return fmt.Errorf("source vector projection is missing %d chunks", len(expected))
	}
	return nil
}

// DeleteChunkIDs не удалит чужой документ или владельца даже при неверном ID.
func (q *Qdrant) DeleteChunkIDs(ctx context.Context, owner, docID string, ids []string) error {
	if owner == "" || docID == "" || len(ids) < 1 || len(ids) > 256 {
		return errors.New("bounded explicit chunk IDs and owner/document required")
	}
	must := []any{map[string]any{"has_id": ids}}
	for key, value := range map[string]string{qOwnerID: owner, "document_id": docID, "node_type": "chunk"} {
		must = append(must, map[string]any{"key": key, "match": map[string]string{qValue: value}})
	}
	body := map[string]any{"filter": map[string]any{qMust: must}}
	return q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/delete?wait=true", body, nil)
}
