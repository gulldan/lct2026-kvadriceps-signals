package dbclient

import (
	"context"
	"time"

	commonv1 "github.com/example/email-parser/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/email-parser/internal/platform/genproto/db/v1"
)

func (c *Client) ClusterHead(ctx context.Context, ownerID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := c.api.GetClusterHead(ctx, &dbv1.GetClusterHeadRequest{OwnerId: ownerID})
	if err != nil {
		return "", mapErr(err)
	}
	return out.GetPublicationId(), nil
}

// PublishClusters публикует полный набор с проверкой ожидаемой головы.
func (c *Client) PublishClusters(ctx context.Context, req *dbv1.PublishClustersRequest) ([]*commonv1.Cluster, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := c.api.PublishClusters(ctx, req)
	if err != nil {
		return nil, mapErr(err)
	}
	return out.GetClusters(), nil
}
