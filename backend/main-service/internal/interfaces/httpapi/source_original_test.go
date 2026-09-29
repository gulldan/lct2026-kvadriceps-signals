package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSourceReadErrorDoesNotLeakStorageAndMapsMissingSnapshot(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{fmt.Errorf("index read: %w", status.Error(codes.NotFound, "private snapshot key")), http.StatusNotFound},
		{status.Error(codes.InvalidArgument, "private query"), http.StatusBadRequest},
		{errors.New("private bucket/key"), http.StatusServiceUnavailable},
	} {
		rr := httptest.NewRecorder()
		sourceReadError(rr, tc.err)
		if rr.Code != tc.code || strings.Contains(rr.Body.String(), "private") {
			t.Fatalf("unsafe source response: status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
}
