package grpcserver

import (
	"context"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
)

func (s *Server) SetDocumentSource(
	ctx context.Context, req *dbv1.SetDocumentSourceRequest,
) (*dbv1.SetDocumentSourceResponse, error) {
	if err := s.svc.SetDocumentSource(ctx, req.GetId(), req.GetOwnerId(), []byte(req.GetSourceMetadata())); err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.SetDocumentSourceResponse{}, nil
}
