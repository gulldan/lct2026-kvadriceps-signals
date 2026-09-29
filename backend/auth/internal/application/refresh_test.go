package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/auth/internal/domain"
)

type scriptedSessions struct {
	loadIdentity *domain.Identity
	loadErr      error
	saveErr      error
	deleteErr    error

	deleted bool
}

func (s *scriptedSessions) Save(_ context.Context, _ string, _ *domain.Identity) error {
	return s.saveErr
}

func (s *scriptedSessions) Rotate(_ context.Context, _, _ string) (*domain.Identity, error) {
	return s.loadIdentity, s.loadErr
}

func (s *scriptedSessions) Delete(_ context.Context, _ string) error {
	s.deleted = true
	return s.deleteErr
}

type erroringIssuer struct{}

func (erroringIssuer) IssueSession(_, _ string, _ []string, _ string) (string, time.Time, error) {
	return "", time.Time{}, errors.New("signer unavailable")
}

func allowedThrottle() domain.LoginThrottle { return &fakeThrottle{allowed: true} }

// noisyThrottle падает на записи и сбросе: ошибка ограничителя не должна ронять вход.
type noisyThrottle struct{}

func (noisyThrottle) Allowed(_ context.Context, _ string) (bool, time.Duration, error) {
	return true, 0, nil
}
func (noisyThrottle) RecordFailure(_ context.Context, _ string) error {
	return errors.New("record failed")
}
func (noisyThrottle) Reset(_ context.Context, _ string) error { return errors.New("reset failed") }

func TestRefreshEmptyToken(t *testing.T) {
	svc := New(&fakeDir{}, &scriptedSessions{}, fakeIssuer{}, allowedThrottle())
	if _, err := svc.Refresh(context.Background(), ""); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Refresh(empty) = %v, want ErrSessionNotFound", err)
	}
}

func TestRefreshSuccess(t *testing.T) {
	sess := &scriptedSessions{loadIdentity: &domain.Identity{UserID: "u1", Username: "bob", Roles: []string{"user"}}}
	svc := New(&fakeDir{}, sess, fakeIssuer{}, allowedThrottle())

	tokens, err := svc.Refresh(context.Background(), "rt-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tokens.AccessToken != "access-token" {
		t.Fatalf("access token = %q", tokens.AccessToken)
	}
	if tokens.RefreshToken == "rt-1" || len(tokens.RefreshToken) != 64 {
		t.Fatalf("refresh token = %q, want rt-1", tokens.RefreshToken)
	}
	if tokens.Identity == nil || tokens.Identity.UserID != "u1" {
		t.Fatalf("identity = %+v", tokens.Identity)
	}
}

func TestRefreshSessionNotFound(t *testing.T) {
	sess := &scriptedSessions{loadErr: domain.ErrSessionNotFound}
	svc := New(&fakeDir{}, sess, fakeIssuer{}, allowedThrottle())
	if _, err := svc.Refresh(context.Background(), "gone"); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Refresh(unknown) = %v, want ErrSessionNotFound", err)
	}
}

func TestRefreshIssuerError(t *testing.T) {
	sess := &scriptedSessions{loadIdentity: &domain.Identity{UserID: "u1"}}
	svc := New(&fakeDir{}, sess, erroringIssuer{}, allowedThrottle())
	_, err := svc.Refresh(context.Background(), "rt-1")
	if err == nil || errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Refresh with failing issuer = %v, want a wrapped signing error", err)
	}
}

func TestLogoutEmptyTokenIsNoop(t *testing.T) {
	sess := &scriptedSessions{}
	svc := New(&fakeDir{}, sess, fakeIssuer{}, allowedThrottle())
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout(empty) = %v, want nil", err)
	}
	if sess.deleted {
		t.Fatal("Logout(empty) must not touch the store")
	}
}

func TestLogoutSuccess(t *testing.T) {
	sess := &scriptedSessions{}
	svc := New(&fakeDir{}, sess, fakeIssuer{}, allowedThrottle())
	if err := svc.Logout(context.Background(), "rt-1"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !sess.deleted {
		t.Fatal("Logout must delete the session")
	}
}

func TestLogoutDeleteError(t *testing.T) {
	sess := &scriptedSessions{deleteErr: errors.New("valkey down")}
	svc := New(&fakeDir{}, sess, fakeIssuer{}, allowedThrottle())
	if err := svc.Logout(context.Background(), "rt-1"); err == nil {
		t.Fatal("Logout must surface a delete failure")
	}
}

func TestLoginIssuerError(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1"}}
	svc := New(dir, &scriptedSessions{}, erroringIssuer{}, allowedThrottle())
	if _, err := svc.Login(context.Background(), "bob", "right", ""); err == nil {
		t.Fatal("expected a signing error from Login")
	}
}

func TestLoginSaveSessionError(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1"}}
	sess := &scriptedSessions{saveErr: errors.New("valkey down")}
	svc := New(dir, sess, fakeIssuer{}, allowedThrottle())
	if _, err := svc.Login(context.Background(), "bob", "right", ""); err == nil {
		t.Fatal("expected a save-session error from Login")
	}
}

func TestLoginRecordFailureErrorIsTolerated(t *testing.T) {
	dir := &fakeDir{authErr: domain.ErrInvalidCredentials}
	svc := New(dir, &scriptedSessions{}, fakeIssuer{}, noisyThrottle{})
	if _, err := svc.Login(context.Background(), "bob", "wrong", ""); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("Login = %v, want ErrInvalidCredentials despite a throttle record error", err)
	}
}

func TestLoginResetErrorIsTolerated(t *testing.T) {
	dir := &fakeDir{authIdentity: &domain.Identity{UserID: "u1", Username: "bob"}}
	svc := New(dir, &scriptedSessions{}, fakeIssuer{}, noisyThrottle{})
	if _, err := svc.Login(context.Background(), "bob", "right", ""); err != nil {
		t.Fatalf("Login = %v, want success despite a throttle reset error", err)
	}
}

func TestRegisterDirectoryError(t *testing.T) {
	dir := &fakeDir{regErr: errors.New("db unreachable")}
	svc := New(dir, &scriptedSessions{}, fakeIssuer{}, allowedThrottle())
	_, err := svc.Register(context.Background(), "carol", "longenough", nil)
	if err == nil || errors.Is(err, domain.ErrUserExists) {
		t.Fatalf("Register = %v, want the underlying directory error", err)
	}
}
