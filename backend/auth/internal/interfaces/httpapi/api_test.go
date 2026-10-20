package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/auth/internal/application"
	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/interfaces/httpapi"
)

type fakeDir struct {
	authIdentity *domain.Identity
	authErr      error
	regIdentity  *domain.Identity
	regErr       error
	gotRoles     []string
}

func (f *fakeDir) Authenticate(_ context.Context, _, _ string) (*domain.Identity, error) {
	return f.authIdentity, f.authErr
}

func (f *fakeDir) Register(_ context.Context, _, _ string, roles []string) (*domain.Identity, error) {
	f.gotRoles = roles
	return f.regIdentity, f.regErr
}

type fakeSessions struct {
	loadIdentity *domain.Identity
	loadErr      error
	saveErr      error
	deleteErr    error
}

func (f *fakeSessions) Save(_ context.Context, _ string, _ *domain.Identity) error {
	return f.saveErr
}

func (f *fakeSessions) Rotate(_ context.Context, _, _ string) (*domain.Identity, error) {
	return f.loadIdentity, f.loadErr
}

func (f *fakeSessions) Delete(_ context.Context, _ string) error { return f.deleteErr }

type fakeIssuer struct{ err error }

func (f fakeIssuer) IssueSession(_, _ string, _ []string, _ string) (string, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return "access-token", time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC), nil
}

type allowThrottle struct{}

func (allowThrottle) Allowed(_ context.Context, _ string) (bool, time.Duration, error) {
	return true, 0, nil
}
func (allowThrottle) RecordFailure(_ context.Context, _ string) error { return nil }
func (allowThrottle) Reset(_ context.Context, _ string) error         { return nil }

type blockThrottle struct{ retryAfter time.Duration }

func (b blockThrottle) Allowed(_ context.Context, _ string) (bool, time.Duration, error) {
	return false, b.retryAfter, nil
}
func (blockThrottle) RecordFailure(_ context.Context, _ string) error { return nil }
func (blockThrottle) Reset(_ context.Context, _ string) error         { return nil }

func newServer(
	t *testing.T,
	dir domain.UserDirectory,
	sess domain.SessionStore,
	iss application.TokenIssuer,
	thr domain.LoginThrottle,
) *httptest.Server {
	t.Helper()
	svc := application.New(dir, sess, iss, thr)
	mux := http.NewServeMux()
	httpapi.New(svc).Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// response вычитывается внутри post, чтобы открытое тело не уходило вызывающему.
type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) decode(t *testing.T) map[string]any {
	t.Helper()
	if len(r.body) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("decode body %q: %v", r.body, err)
	}
	return m
}

func post(t *testing.T, srv *httptest.Server, path, body string, headers map[string]string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: raw}
}

func TestLoginSuccess(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1", Username: "bob", Roles: []string{"user"}}}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, allowThrottle{})

	resp := post(t, srv, "/login", `{"username":"bob","password":"secret"}`, nil)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.status)
	}
	body := resp.decode(t)
	if body["access_token"] != "access-token" || body["token_type"] != "Bearer" {
		t.Fatalf("unexpected token body: %v", body)
	}
	if body["refresh_token"] == "" || body["refresh_token"] == nil {
		t.Fatalf("expected a refresh token, got %v", body["refresh_token"])
	}
	user, ok := body["user"].(map[string]any)
	if !ok || user["id"] != "u1" || user["username"] != "bob" {
		t.Fatalf("unexpected user block: %v", body["user"])
	}
}

func TestLoginInvalidJSON(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/login", `{not json`, nil)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.status)
	}
}

func TestLoginMissingFields(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	cases := []string{`{"username":"","password":"x"}`, `{"username":"bob","password":""}`, `{}`}
	for _, body := range cases {
		resp := post(t, srv, "/login", body, nil)
		if resp.status != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, resp.status)
		}
		if got := resp.decode(t)["error"]; got != "username and password are required" {
			t.Fatalf("body %q: error = %v", body, got)
		}
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	dir := &fakeDir{authErr: domain.ErrInvalidCredentials}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/login", `{"username":"bob","password":"wrong"}`, nil)
	if resp.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.status)
	}
	if got := resp.decode(t)["error"]; got != "invalid credentials" {
		t.Fatalf("error = %v", got)
	}
}

func TestLoginTooManyAttempts(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1"}}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, blockThrottle{retryAfter: 90 * time.Second})
	resp := post(t, srv, "/login", `{"username":"bob","password":"secret"}`, nil)
	if resp.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.status)
	}
	if got := resp.header.Get("Retry-After"); got != "90" {
		t.Fatalf("Retry-After = %q, want 90", got)
	}
}

func TestLoginTooManyAttemptsRetryAfterRounding(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1"}}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, blockThrottle{retryAfter: 200 * time.Millisecond})
	resp := post(t, srv, "/login", `{"username":"bob","password":"secret"}`, nil)
	if resp.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.status)
	}
	if got := resp.header.Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

func TestLoginInternalError(t *testing.T) {
	dir := &fakeDir{authErr: errors.New("db unreachable")}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/login", `{"username":"bob","password":"secret"}`, nil)
	if resp.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.status)
	}
	if got := resp.decode(t)["error"]; got != "internal error" {
		t.Fatalf("error = %v, want generic message", got)
	}
}

func TestLoginUsesProxyHeaders(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1", Username: "bob"}}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	for _, hdr := range []map[string]string{
		{"X-Real-IP": "203.0.113.7"},
		{"X-Forwarded-For": "203.0.113.9, 198.51.100.10"},
		{"X-Forwarded-For": "  198.51.100.2  "},
	} {
		resp := post(t, srv, "/login", `{"username":"bob","password":"secret"}`, hdr)
		if resp.status != http.StatusOK {
			t.Fatalf("header %v: status = %d, want 200", hdr, resp.status)
		}
	}
}

func TestRefreshSuccess(t *testing.T) {
	sess := &fakeSessions{loadIdentity: &domain.Identity{UserID: "u9", Username: "ann", Roles: []string{"user"}}}
	srv := newServer(t, &fakeDir{}, sess, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/refresh", `{"refresh_token":"rt-123"}`, nil)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.status)
	}
	body := resp.decode(t)
	if body["access_token"] != "access-token" {
		t.Fatalf("access_token = %v", body["access_token"])
	}
	if body["refresh_token"] == "rt-123" || body["refresh_token"] == "" {
		t.Fatalf("refresh_token = %v, want rt-123", body["refresh_token"])
	}
}

func TestRefreshInvalidJSON(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/refresh", `{`, nil)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.status)
	}
}

func TestRefreshMissingToken(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/refresh", `{"refresh_token":""}`, nil)
	if resp.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.status)
	}
	if got := resp.decode(t)["error"]; got != "refresh session required" {
		t.Fatalf("error = %v", got)
	}
	browser := post(t, srv, "/refresh", `{}`, map[string]string{"X-Auth-Mode": "browser", "Origin": srv.URL})
	if browser.status != http.StatusNoContent {
		t.Fatalf("browser without cookie: status = %d, want 204", browser.status)
	}
}

func TestRefreshUnknownSession(t *testing.T) {
	sess := &fakeSessions{loadErr: domain.ErrSessionNotFound}
	srv := newServer(t, &fakeDir{}, sess, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/refresh", `{"refresh_token":"gone"}`, nil)
	if resp.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.status)
	}
	if got := resp.decode(t)["error"]; got != "invalid or expired refresh token" {
		t.Fatalf("error = %v", got)
	}
}

func TestBrowserSessionCookieAndCrossOriginRejection(t *testing.T) {
	identity := &domain.Identity{UserID: "u1", Username: "reader", SessionExpiresAt: time.Now().Add(time.Hour)}
	srv := newServer(t, &fakeDir{authIdentity: identity}, &fakeSessions{loadIdentity: identity}, fakeIssuer{}, allowThrottle{})
	headers := map[string]string{"X-Auth-Mode": "browser", "Origin": srv.URL}
	resp := post(t, srv, "/login", `{"username":"reader","password":"password"}`, headers)
	if resp.status != http.StatusOK {
		t.Fatalf("login: %d", resp.status)
	}
	if _, exposed := resp.decode(t)["refresh_token"]; exposed {
		t.Fatal("refresh secret exposed to JavaScript")
	}
	cookies := (&http.Response{Header: resp.header}).Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe cookie: %+v", cookies)
	}
	headers["Cookie"] = cookies[0].Name + "=" + cookies[0].Value
	resp = post(t, srv, "/refresh", `{}`, headers)
	if resp.status != http.StatusOK || resp.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("refresh: %+v", resp)
	}
	headers["Origin"] = "https://attacker.example"
	for _, path := range []string{"/login", "/refresh", "/logout"} {
		resp = post(t, srv, path, `{}`, headers)
		if resp.status != http.StatusForbidden {
			t.Fatalf("cross-origin %s: %d", path, resp.status)
		}
	}
}

func TestRefreshIssuerError(t *testing.T) {
	sess := &fakeSessions{loadIdentity: &domain.Identity{UserID: "u9"}}
	srv := newServer(t, &fakeDir{}, sess, fakeIssuer{err: errors.New("signer down")}, allowThrottle{})
	resp := post(t, srv, "/refresh", `{"refresh_token":"rt"}`, nil)
	if resp.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.status)
	}
}

func TestLogoutSuccess(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/logout", `{"refresh_token":"rt-1"}`, nil)
	if resp.status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.status)
	}
}

func TestLogoutEmptyTokenIsNoop(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/logout", `{"refresh_token":""}`, nil)
	if resp.status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.status)
	}
}

func TestLogoutInvalidJSON(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/logout", `nope`, nil)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.status)
	}
}

func TestLogoutDeleteError(t *testing.T) {
	sess := &fakeSessions{deleteErr: errors.New("valkey down")}
	srv := newServer(t, &fakeDir{}, sess, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/logout", `{"refresh_token":"rt"}`, nil)
	if resp.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.status)
	}
}

func TestRegisterSuccess(t *testing.T) {
	dir := &fakeDir{regIdentity: &domain.Identity{UserID: "u2", Username: "carol", Roles: []string{"user"}}}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/register", `{"username":"carol","password":"longenough","roles":["admin"]}`, nil)
	if resp.status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.status)
	}
	if len(dir.gotRoles) != 1 || dir.gotRoles[0] != "user" {
		t.Fatalf("anonymous register must get [user], got %v", dir.gotRoles)
	}
	body := resp.decode(t)
	if body["access_token"] != "access-token" {
		t.Fatalf("access_token = %v", body["access_token"])
	}
}

func TestRegisterInvalidJSON(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/register", `{bad`, nil)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.status)
	}
}

func TestRegisterMissingFields(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/register", `{"username":"carol","password":""}`, nil)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.status)
	}
	if got := resp.decode(t)["error"]; got != "username and password are required" {
		t.Fatalf("error = %v", got)
	}
}

func TestRegisterWeakPassword(t *testing.T) {
	srv := newServer(t, &fakeDir{}, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/register", `{"username":"carol","password":"short"}`, nil)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.status)
	}
	msg, _ := resp.decode(t)["error"].(string)
	if !strings.Contains(msg, "at least") {
		t.Fatalf("error = %q, want password-policy reason", msg)
	}
}

func TestRegisterDuplicateUser(t *testing.T) {
	dir := &fakeDir{regErr: domain.ErrUserExists}
	srv := newServer(t, dir, &fakeSessions{}, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/register", `{"username":"carol","password":"longenough"}`, nil)
	if resp.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.status)
	}
	if got := resp.decode(t)["error"]; got != "username already taken" {
		t.Fatalf("error = %v", got)
	}
}

func TestRegisterSaveSessionError(t *testing.T) {
	dir := &fakeDir{regIdentity: &domain.Identity{UserID: "u2", Username: "carol"}}
	sess := &fakeSessions{saveErr: errors.New("valkey down")}
	srv := newServer(t, dir, sess, fakeIssuer{}, allowThrottle{})
	resp := post(t, srv, "/register", `{"username":"carol","password":"longenough"}`, nil)
	if resp.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.status)
	}
}
