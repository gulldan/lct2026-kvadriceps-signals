package searchstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ConfirmIndex читает в реальном времени; Refresh делает проверенную проекцию доступной для поиска
// до публикации указателя готовности в базе.
func (o *OpenSearch) ConfirmIndex(ctx context.Context, owner, docID, snapshot string, ids []string) error {
	if owner == "" || docID == "" || snapshot == "" || len(ids) < 1 || len(ids) > 256 {
		return errors.New("invalid source lexical confirmation")
	}
	var out struct {
		Docs []struct {
			ID     string `json:"_id"`
			Found  bool   `json:"found"`
			Source Doc    `json:"_source"`
		} `json:"docs"`
	}
	if err := o.do(ctx, http.MethodPost, "/"+o.index+"/_mget", map[string]any{"ids": ids}, &out); err != nil {
		return err
	}
	expected := make(map[string]bool, len(ids))
	for _, id := range ids {
		expected[id] = true
	}
	for _, item := range out.Docs {
		if !expected[item.ID] || !item.Found || item.Source.OwnerID != owner || item.Source.DocumentID != docID ||
			item.Source.Metadata["source_index_snapshot"] != snapshot {
			return errors.New("source lexical projection differs from its manifest")
		}
		delete(expected, item.ID)
	}
	if len(expected) != 0 {
		return fmt.Errorf("source lexical projection is missing %d chunks", len(expected))
	}
	return nil
}

func (o *OpenSearch) Refresh(ctx context.Context) error {
	return o.do(ctx, http.MethodPost, "/"+o.index+"/_refresh", nil, nil)
}

// DeleteChunkIDs удаляет по явным ID И владельцу/документу, никогда по всему корпусу.
func (o *OpenSearch) DeleteChunkIDs(ctx context.Context, owner, docID string, ids []string) error {
	if owner == "" || docID == "" || len(ids) < 1 || len(ids) > 256 {
		return errors.New("bounded explicit chunk IDs and owner/document required")
	}
	filters := []any{
		map[string]any{"ids": map[string]any{"values": ids}},
		map[string]any{osTerm: map[string]string{osOwnerID: owner}},
		map[string]any{osTerm: map[string]string{"document_id": docID}},
	}
	body := map[string]any{osQuery: map[string]any{"bool": map[string]any{"filter": filters}}}
	var result struct {
		TimedOut  bool  `json:"timed_out"`
		Conflicts int   `json:"version_conflicts"`
		Failures  []any `json:"failures"`
	}
	if err := o.do(ctx, http.MethodPost, "/"+o.index+"/_delete_by_query?refresh=true", body, &result); err != nil {
		return err
	}
	if result.TimedOut || result.Conflicts != 0 || len(result.Failures) != 0 {
		return errors.New("incomplete source lexical prune")
	}
	return nil
}
