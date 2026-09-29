package grpcx_test

import (
	"context"
	"sync/atomic"
	"testing"

	commonv1 "github.com/example/vlm-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/vlm-service/internal/platform/genproto/db/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ambiguousWriteServer struct {
	dbv1.UnimplementedDbServiceServer
	writes atomic.Int32
	reads  atomic.Int32
}

func (s *ambiguousWriteServer) CreateUser(context.Context, *dbv1.CreateUserRequest) (*commonv1.User, error) {
	s.writes.Add(1)
	return nil, status.Error(codes.Unavailable, "response lost after commit")
}

func (s *ambiguousWriteServer) GetDocument(context.Context, *dbv1.GetDocumentRequest) (*commonv1.Document, error) {
	if s.reads.Add(1) == 1 {
		return nil, status.Error(codes.Unavailable, "temporary read failure")
	}
	return &commonv1.Document{Id: "doc"}, nil
}

func TestOnlyExplicitReadsRetryAfterApplicationFailure(t *testing.T) {
	s := &ambiguousWriteServer{}
	addr := startServer(t, func(r grpc.ServiceRegistrar) { dbv1.RegisterDbServiceServer(r, s) })
	c := dbv1.NewDbServiceClient(dialReady(t, addr))
	if _, err := c.CreateUser(
		t.Context(),
		&dbv1.CreateUserRequest{},
	); status.Code(
		err,
	) != codes.Unavailable ||
		s.writes.Load() != 1 {
		t.Fatalf("ambiguous write retried: calls=%d err=%v", s.writes.Load(), err)
	}
	if _, err := c.GetDocument(t.Context(), &dbv1.GetDocumentRequest{Id: "doc"}); err != nil || s.reads.Load() != 2 {
		t.Fatalf("safe read did not recover: calls=%d err=%v", s.reads.Load(), err)
	}
}
