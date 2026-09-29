package grpcserver

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func controlPlaneFault(err error, invalid, conflict, notFound error, subject string) error {
	switch {
	case errors.Is(err, invalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, conflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, notFound):
		return status.Error(codes.NotFound, subject+" unavailable")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, subject+" canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, subject+" timed out")
	default:
		return status.Error(codes.Internal, subject+" storage failed")
	}
}
