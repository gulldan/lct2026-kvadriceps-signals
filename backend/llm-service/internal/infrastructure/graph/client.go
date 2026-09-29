package graph

import (
	"context"
	"fmt"
	"time"

	graphv1 "github.com/example/llm-service/internal/platform/genproto/graph/v1"
)

type Client struct {
	client     graphv1.GraphComputeClient
	collection string
	timeout    time.Duration
}

func NewClient(client graphv1.GraphComputeClient, collection string, timeout time.Duration) *Client {
	return &Client{client: client, collection: collection, timeout: timeout}
}

// Expand без затравок ничего не возвращает: пустой seed запустил бы PageRank по всему корпусу.
func (c *Client) Expand(ctx context.Context, ownerID string, seedDocIDs []string, topN int) ([]string, error) {
	if len(seedDocIDs) == 0 {
		return []string{}, nil
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	resp, err := c.client.Rank(ctx, &graphv1.RankRequest{
		OwnerId:    ownerID,
		Collection: c.collection,
		SeedIds:    seedDocIDs,
		TopN:       uint32(topN),
	})
	if err != nil {
		return nil, fmt.Errorf("graph rank: %w", err)
	}
	nodes := resp.GetNodes()
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id := n.GetId(); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
