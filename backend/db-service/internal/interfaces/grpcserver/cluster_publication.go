package grpcserver

import (
	"context"

	"github.com/example/db-service/internal/domain"
	commonv1 "github.com/example/db-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
)

func (s *Server) GetClusterHead(ctx context.Context, req *dbv1.GetClusterHeadRequest) (*dbv1.GetClusterHeadResponse, error) {
	id, err := s.svc.ClusterHead(ctx, req.GetOwnerId())
	if err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.GetClusterHeadResponse{PublicationId: id}, nil
}

func (s *Server) PublishClusters(ctx context.Context, req *dbv1.PublishClustersRequest) (*dbv1.ListClustersResponse, error) {
	clusters := make([]*domain.Cluster, 0, len(req.GetClusters()))
	for _, c := range req.GetClusters() {
		clusters = append(clusters, &domain.Cluster{Label: c.GetLabel(), Summary: c.GetSummary(), Keywords: c.GetKeywords(),
			Method: c.GetMethod(), ChunkCount: int(c.GetChunkCount()), DocumentCount: int(c.GetDocumentCount()),
			Representatives: []byte(c.GetRepresentatives()), Params: []byte(c.GetParams())})
	}
	created, err := s.svc.PublishClusters(ctx, req.GetOwnerId(), req.GetRequestId(), req.GetExpectedPublicationId(), clusters)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*commonv1.Cluster, 0, len(created))
	for _, c := range created {
		out = append(out, clusterPB(c))
	}
	return &dbv1.ListClustersResponse{Clusters: out}, nil
}
