package lexicalindex

import (
	"context"

	"github.com/example/chunk-splitter/internal/application"
	"github.com/example/chunk-splitter/internal/platform/searchstore"
)

type Adapter struct {
	client *searchstore.OpenSearch
}

func New(client *searchstore.OpenSearch) *Adapter {
	return &Adapter{client: client}
}

// Index кладёт раздел в Metadata, чтобы попадания BM25 несли тот же провенанс, что и векторные.
func (a *Adapter) Index(ctx context.Context, docs []application.SearchDoc) error {
	out := make([]searchstore.Doc, 0, len(docs))
	for _, d := range docs {
		meta := copyMetadata(d.Metadata)
		if d.Section != "" {
			if meta == nil {
				meta = map[string]string{}
			}
			meta["section"] = d.Section
		}
		out = append(out, searchstore.Doc{
			SourceText: d.SourceText,
			ID:         d.ID,
			Text:       d.Text,
			DocumentID: d.DocumentID,
			OwnerID:    d.OwnerID,
			Filename:   d.Filename,
			Metadata:   meta,
		})
	}
	return a.client.BulkIndex(ctx, out)
}

func copyMetadata(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func (a *Adapter) Confirm(ctx context.Context, owner, docID, snapshot string, ids []string) error {
	return a.client.ConfirmIndex(ctx, owner, docID, snapshot, ids)
}

func (a *Adapter) Refresh(ctx context.Context) error { return a.client.Refresh(ctx) }

func (a *Adapter) DeleteIDs(ctx context.Context, owner, docID string, ids []string) error {
	return a.client.DeleteChunkIDs(ctx, owner, docID, ids)
}
