package grpcserver

import (
	"context"
	"encoding/json/v2"

	harvestv1 "github.com/example/db-service/internal/platform/genproto/harvest/v1"
	"github.com/example/db-service/internal/platform/harvest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HarvestStore — только хранимая плоскость управления: внешних URL не загружает.
type HarvestStore interface {
	Start(context.Context, harvest.Start) (harvest.Job, error)
	Quota(context.Context, harvest.Quota) (harvest.Grant, error)
	CommitPage(context.Context, harvest.CommitPage) (harvest.Job, error)
	Pending(context.Context, harvest.Pending) (harvest.PendingResult, error)
	Ack(context.Context, harvest.Ack) (harvest.PendingResult, error)
	Finish(context.Context, harvest.Finish) (harvest.Job, error)
	Request(context.Context, harvest.Request) (harvest.Collection, error)
	Requested(context.Context, harvest.Requested) ([]harvest.Collection, error)
	Progress(context.Context, harvest.Progress) (harvest.Collection, error)
}

type HarvestServer struct {
	harvestv1.UnimplementedHarvestServiceServer
	store HarvestStore
}

func NewHarvest(store HarvestStore) *HarvestServer { return &HarvestServer{store: store} }

func harvestCall[In, Out any](
	ctx context.Context, req *harvestv1.Envelope, call func(context.Context, In) (Out, error),
) (*harvestv1.Envelope, error) {
	in, err := harvest.Decode[In](req.GetJson())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	out, err := call(ctx, in)
	if err != nil {
		return nil, controlPlaneFault(err, harvest.ErrInvalid, harvest.ErrConflict, harvest.ErrNotFound, "collection")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, status.Error(codes.Internal, "encode collection result")
	}
	return &harvestv1.Envelope{Json: string(raw)}, nil
}

func (s *HarvestServer) Start(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Start)
}

func (s *HarvestServer) Quota(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Quota)
}

func (s *HarvestServer) CommitPage(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.CommitPage)
}

func (s *HarvestServer) Pending(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Pending)
}

func (s *HarvestServer) Ack(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Ack)
}

func (s *HarvestServer) Finish(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Finish)
}

func (s *HarvestServer) Request(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Request)
}

func (s *HarvestServer) Requested(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Requested)
}

func (s *HarvestServer) Progress(c context.Context, r *harvestv1.Envelope) (*harvestv1.Envelope, error) {
	return harvestCall(c, r, s.store.Progress)
}
