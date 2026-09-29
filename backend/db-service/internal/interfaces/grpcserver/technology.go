package grpcserver

import (
	"context"
	"encoding/json/v2"

	"github.com/example/db-service/internal/platform/extractdoc"
	technologyv1 "github.com/example/db-service/internal/platform/genproto/technology/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type TechnologyStore interface {
	ReadRevision(context.Context, extractdoc.ReadRevision) (extractdoc.RevisionTechnologies, error)
	ReadTechnology(context.Context, extractdoc.ReadTechnology) (extractdoc.Technology, error)
	SearchTechnologies(context.Context, extractdoc.SearchTechnologies) (extractdoc.TechnologyPage, error)
	Review(context.Context, extractdoc.Review) (extractdoc.Review, error)
	Reviews(context.Context, extractdoc.ReadReviews) ([]extractdoc.Review, error)
}

type TechnologyServer struct {
	technologyv1.UnimplementedTechnologyServiceServer
	store TechnologyStore
}

func NewTechnology(store TechnologyStore) *TechnologyServer {
	return &TechnologyServer{store: store}
}

func technologyCall[In, Out any](ctx context.Context, req *technologyv1.Envelope,
	call func(context.Context, In) (Out, error), subject string,
) (*technologyv1.Envelope, error) {
	in, err := extractdoc.Decode[In](req.GetJson())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	out, err := call(ctx, in)
	if err != nil {
		return nil, controlPlaneFault(err, extractdoc.ErrInvalid, extractdoc.ErrConflict,
			extractdoc.ErrNotFound, subject)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, status.Error(codes.Internal, "encode "+subject)
	}
	return &technologyv1.Envelope{Json: string(raw)}, nil
}

func (s *TechnologyServer) ReadRevision(ctx context.Context, req *technologyv1.Envelope,
) (*technologyv1.Envelope, error) {
	return technologyCall(ctx, req, s.store.ReadRevision, "revision technologies")
}

func (s *TechnologyServer) Review(ctx context.Context, req *technologyv1.Envelope,
) (*technologyv1.Envelope, error) {
	return technologyCall(ctx, req, s.store.Review, "technology review")
}

func (s *TechnologyServer) Reviews(ctx context.Context, req *technologyv1.Envelope,
) (*technologyv1.Envelope, error) {
	return technologyCall(ctx, req, s.store.Reviews, "technology review history")
}

func (s *TechnologyServer) ReadTechnology(ctx context.Context, req *technologyv1.Envelope,
) (*technologyv1.Envelope, error) {
	return technologyCall(ctx, req, s.store.ReadTechnology, "technology")
}

func (s *TechnologyServer) Search(ctx context.Context, req *technologyv1.Envelope,
) (*technologyv1.Envelope, error) {
	return technologyCall(ctx, req, s.store.SearchTechnologies, "technology catalogue")
}
