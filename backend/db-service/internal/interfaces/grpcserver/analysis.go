package grpcserver

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/example/db-service/internal/platform/analysisdoc"
	analysisv1 "github.com/example/db-service/internal/platform/genproto/analysis/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AnalysisStore interface {
	Freeze(context.Context, analysisdoc.Request) (analysisdoc.Frozen, error)
	Read(context.Context, analysisdoc.Read) (analysisdoc.Frozen, error)
	Submit(context.Context, analysisdoc.Submit) (analysisdoc.Job, error)
	ReadJob(context.Context, analysisdoc.JobRead) (analysisdoc.Job, error)
	Claim(context.Context, analysisdoc.Claim) (analysisdoc.Claimed, error)
	Renew(context.Context, analysisdoc.Lease) (analysisdoc.Job, error)
	Validate(context.Context, analysisdoc.Validate) (analysisdoc.Job, error)
	Publish(context.Context, analysisdoc.Lease) (analysisdoc.Job, error)
	Fail(context.Context, analysisdoc.Failure) (analysisdoc.Job, error)
	StartDescription(context.Context, analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error)
	ReadDescription(context.Context, analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error)
	ClaimDescription(context.Context, analysisdoc.DescriptionClaim) (analysisdoc.DescriptionClaimed, error)
	CompleteDescription(context.Context, analysisdoc.DescriptionComplete) (analysisdoc.DescriptionJob, error)
	ReadLiveSearch(context.Context, analysisdoc.LiveSearchRead) (analysisdoc.LiveSearchStored, error)
	SaveLiveSearch(context.Context, analysisdoc.LiveSearchSave) (analysisdoc.LiveSearchStored, error)
	ListLiveSearches(context.Context, analysisdoc.LiveSearchList) (analysisdoc.LiveSearchHistory, error)
}

type AnalysisServer struct {
	analysisv1.UnimplementedAnalysisServiceServer
	store AnalysisStore
}

func NewAnalysis(store AnalysisStore) *AnalysisServer { return &AnalysisServer{store: store} }

func analysisCall[In, Out any](ctx context.Context, req *analysisv1.Envelope,
	call func(context.Context, In) (Out, error),
) (*analysisv1.Envelope, error) {
	in, err := analysisdoc.Decode[In](req.GetJson())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid analysis request")
	}
	out, err := call(ctx, in)
	if err != nil {
		switch {
		case errors.Is(err, analysisdoc.ErrHistoryFull):
			return nil, status.Error(codes.FailedPrecondition, "analysis history full")
		case errors.Is(err, analysisdoc.ErrCapacity):
			return nil, status.Error(codes.ResourceExhausted, "analysis queue full")
		case errors.Is(err, analysisdoc.ErrInvalid):
			// Причина идёт вместе с кодом: «превышен лимит охвата» и «слишком большой вход» —
			// разные действия оператора.
			return nil, status.Error(codes.InvalidArgument, "invalid analysis input: "+err.Error())
		case errors.Is(err, analysisdoc.ErrConflict):
			return nil, status.Error(codes.Aborted, "analysis request conflicts with its frozen input")
		case errors.Is(err, analysisdoc.ErrAccessChanged):
			return nil, status.Error(codes.PermissionDenied, "access_changed")
		case errors.Is(err, analysisdoc.ErrNotFound):
			return nil, status.Error(codes.NotFound, "analysis unavailable")
		case errors.Is(err, context.Canceled):
			return nil, status.Error(codes.Canceled, "analysis canceled")
		case errors.Is(err, context.DeadlineExceeded):
			return nil, status.Error(codes.DeadlineExceeded, "analysis timed out")
		default:
			return nil, status.Error(codes.Internal, "analysis storage failed")
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, status.Error(codes.Internal, "encode analysis result")
	}
	return &analysisv1.Envelope{Json: string(raw)}, nil
}

func (s *AnalysisServer) Freeze(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Freeze)
}

func (s *AnalysisServer) Read(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Read)
}

func (s *AnalysisServer) Submit(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Submit)
}

func (s *AnalysisServer) ReadJob(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.ReadJob)
}

func (s *AnalysisServer) Claim(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Claim)
}

func (s *AnalysisServer) Renew(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Renew)
}

func (s *AnalysisServer) Validate(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Validate)
}

func (s *AnalysisServer) Publish(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Publish)
}

func (s *AnalysisServer) Fail(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.Fail)
}

func (s *AnalysisServer) StartDescription(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.StartDescription)
}

func (s *AnalysisServer) ReadDescription(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.ReadDescription)
}

func (s *AnalysisServer) ClaimDescription(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.ClaimDescription)
}

func (s *AnalysisServer) CompleteDescription(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.CompleteDescription)
}

func (s *AnalysisServer) ListLiveSearches(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.ListLiveSearches)
}

func (s *AnalysisServer) ReadLiveSearch(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.ReadLiveSearch)
}

func (s *AnalysisServer) SaveLiveSearch(c context.Context, r *analysisv1.Envelope) (*analysisv1.Envelope, error) {
	return analysisCall(c, r, s.store.SaveLiveSearch)
}
