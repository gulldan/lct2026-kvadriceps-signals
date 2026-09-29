package application

import (
	"context"
	"strconv"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func (c *fakeCatalog) ClusterHead(context.Context, string) (string, error) { return "", nil }

func (c *fakeCatalog) PublishClusters(_ context.Context, req *dbv1.PublishClustersRequest) ([]*commonv1.Cluster, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, old := range c.clusters {
		if old.GetOwnerId() == req.GetOwnerId() {
			delete(c.clusters, id)
		}
	}
	out := make([]*commonv1.Cluster, 0, len(req.GetClusters()))
	for i, in := range req.GetClusters() {
		cl := &commonv1.Cluster{Id: req.GetRequestId() + "-" + strconv.Itoa(i), OwnerId: req.GetOwnerId(),
			Label: in.GetLabel(), Summary: in.GetSummary(), Params: in.GetParams()}
		c.clusters[cl.GetId()] = cl
		out = append(out, cl)
	}
	return out, nil
}
