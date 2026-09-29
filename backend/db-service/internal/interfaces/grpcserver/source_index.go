package grpcserver

import (
	"context"
	"encoding/json/v2"

	indexdocv1 "github.com/example/db-service/internal/platform/genproto/indexdoc/v1"
	"github.com/example/db-service/internal/platform/indexdoc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type IndexStore interface {
	Begin(context.Context, indexdoc.Lease) (indexdoc.Job, error)
	Renew(context.Context, indexdoc.Lease) (indexdoc.Empty, error)
	Prepare(context.Context, indexdoc.Activation) (indexdoc.Empty, error)
	Activate(context.Context, indexdoc.Activation) (indexdoc.Empty, error)
	Finish(context.Context, indexdoc.Finish) (indexdoc.Empty, error)
	Read(context.Context, indexdoc.Read) (indexdoc.States, error)
	SetAccess(context.Context, indexdoc.AccessChange) (indexdoc.Empty, error)
}

type IndexServer struct {
	indexdocv1.UnimplementedIndexServiceServer
	store IndexStore
}

func NewIndex(store IndexStore) *IndexServer { return &IndexServer{store: store} }

func indexCall[In, Out any](
	ctx context.Context, req *indexdocv1.Envelope, call func(context.Context, In) (Out, error),
) (*indexdocv1.Envelope, error) {
	in, err := indexdoc.Decode[In](req.GetJson())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	out, err := call(ctx, in)
	if err != nil {
		return nil, controlPlaneFault(err, indexdoc.ErrInvalid, indexdoc.ErrConflict, indexdoc.ErrNotFound, "source index")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, status.Error(codes.Internal, "encode source index result")
	}
	if len(raw) > indexdoc.MaxResponseBytes {
		return nil, status.Error(codes.ResourceExhausted, "source index response exceeds its byte budget")
	}
	return &indexdocv1.Envelope{Json: string(raw)}, nil
}

func (s *IndexServer) Begin(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.Begin)
}

func (s *IndexServer) Renew(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.Renew)
}

func (s *IndexServer) Prepare(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.Prepare)
}

func (s *IndexServer) Activate(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.Activate)
}

func (s *IndexServer) Finish(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.Finish)
}

func (s *IndexServer) Read(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.Read)
}

func (s *IndexServer) SetAccess(c context.Context, r *indexdocv1.Envelope) (*indexdocv1.Envelope, error) {
	return indexCall(c, r, s.store.SetAccess)
}
