package jwt_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/jwt"
)

func TestSessionRevocationIsCheckedOnEveryRequest(t *testing.T) {
	m := jwt.NewManager("test-secret", "test", time.Hour)
	token, _, err := m.IssueSession("reader", "reader", []string{"admin"}, "session")
	if err != nil {
		t.Fatal(err)
	}
	var failure error
	m.SetSessionValidator(func(_ context.Context, c *jwt.Claims) error {
		if c.SessionID != "session" {
			t.Fatal("session binding lost")
		}
		c.Roles = []string{"user"}
		return failure
	})
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := jwt.ClaimsFromContext(r.Context())
		if len(claims.Roles) != 1 || claims.Roles[0] != "user" {
			t.Fatal("stale roles")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, step := range []struct {
		failure error
		status  int
	}{
		{nil, http.StatusNoContent}, {errors.New("revoked"), http.StatusUnauthorized},
		{jwt.ErrSessionUnavailable, http.StatusServiceUnavailable},
	} {
		failure = step.failure
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != step.status {
			t.Fatalf("status: %d want %d", response.Code, step.status)
		}
	}
}
