package dbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/vlm-service/internal/platform/contracts"
	dbv1 "github.com/example/vlm-service/internal/platform/genproto/db/v1"
	"time"
)

func documentPageRequest(q contracts.DocumentQuery) *dbv1.DocumentPageRequest {
	return &dbv1.DocumentPageRequest{ReaderId: q.ReaderID, AllOwners: q.AllOwners, Query: q.Query, Status: q.Status,
		FileType: q.FileType, BeforeId: q.BeforeID, Limit: int32(q.Limit), Offset: int32(q.Offset)}
}

func (c *Client) DocumentPage(ctx context.Context, q contracts.DocumentQuery) (*dbv1.DocumentPageResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := dbv1.NewDocumentQueriesClient(c.conn).Page(ctx, documentPageRequest(q))
	return out, mapErr(err)
}

func (c *Client) DocumentStats(ctx context.Context, q contracts.DocumentQuery) (contracts.DocumentStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var stats contracts.DocumentStats
	out, err := dbv1.NewDocumentQueriesClient(c.conn).Stats(ctx, documentPageRequest(q))
	if err != nil {
		return stats, mapErr(err)
	}
	if err = json.Unmarshal(out.GetJson(), &stats); err != nil {
		return stats, fmt.Errorf("decode document stats: %w", err)
	}
	return stats, nil
}
