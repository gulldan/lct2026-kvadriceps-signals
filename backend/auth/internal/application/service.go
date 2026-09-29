package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/platform/logger"
)

// bcrypt молча обрезает ввод длиннее 72 байт: длинный пароль отклоняем, а не хешируем префикс.
const (
	minPasswordLen = 8
	maxPasswordLen = 72
)

const defaultRole = "user"

type TokenIssuer interface {
	IssueSession(userID, username string, roles []string, sessionID string) (string, time.Time, error)
}

type AuthService struct {
	dir      domain.UserDirectory
	sessions domain.SessionStore
	issuer   TokenIssuer
	throttle domain.LoginThrottle
}

func New(
	dir domain.UserDirectory,
	sessions domain.SessionStore,
	issuer TokenIssuer,
	throttle domain.LoginThrottle,
) *AuthService {
	return &AuthService{dir: dir, sessions: sessions, issuer: issuer, throttle: throttle}
}

type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Identity     *domain.Identity
}

// Login спрашивает ограничитель до bcrypt, чтобы сдерживать и перебор, и его цену по CPU.
func (s *AuthService) Login(ctx context.Context, username, password, clientIP string) (*Tokens, error) {
	key := throttleKey(username, clientIP)
	log := logger.From(ctx)

	if ok, retryAfter, err := s.throttle.Allowed(ctx, key); err != nil {
		log.Warn().Err(err).Msg("login throttle unavailable; allowing attempt")
	} else if !ok {
		return nil, &domain.TooManyAttemptsError{RetryAfter: retryAfter}
	}

	identity, err := s.dir.Authenticate(ctx, username, password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			if rerr := s.throttle.RecordFailure(ctx, key); rerr != nil {
				log.Warn().Err(rerr).Msg("login throttle: record failure failed")
			}
		}
		return nil, err
	}

	if rerr := s.throttle.Reset(ctx, key); rerr != nil {
		log.Warn().Err(rerr).Msg("login throttle: reset failed")
	}
	return s.issue(ctx, identity)
}

func throttleKey(username, clientIP string) string {
	if clientIP == "" {
		return "user:" + username
	}
	return "user:" + username + "|ip:" + clientIP
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if refreshToken == "" {
		return nil, domain.ErrSessionNotFound
	}
	next := refreshTokenValue()
	identity, err := s.sessions.Rotate(ctx, refreshToken, next)
	if err != nil {
		return nil, err
	}
	access, exp, err := s.issuer.IssueSession(identity.UserID, identity.Username, identity.Roles, identity.SessionID)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}
	return &Tokens{
		AccessToken:  access,
		RefreshToken: next,
		ExpiresAt:    exp,
		Identity:     identity,
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	if err := s.sessions.Delete(ctx, refreshToken); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// Register оставляет из запрошенных ролей только известные, а без них выдаёт defaultRole. Кому
// можно просить роли, решает HTTP-слой: анонимная регистрация передаёт nil.
func (s *AuthService) Register(ctx context.Context, username, password string, roles []string) (*Tokens, error) {
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	granted := slices.DeleteFunc(slices.Clone(roles), func(r string) bool {
		return !slices.Contains([]string{"user", "operator", "admin"}, r)
	})
	if len(granted) == 0 {
		granted = []string{defaultRole}
	}
	identity, err := s.dir.Register(ctx, username, password, granted)
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, identity)
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("%w: must be at least %d characters", domain.ErrWeakPassword, minPasswordLen)
	}
	if len(password) > maxPasswordLen {
		return fmt.Errorf("%w: must be at most %d bytes", domain.ErrWeakPassword, maxPasswordLen)
	}
	return nil
}

// Сначала сохраняем сессию: подписанный токен не должен существовать без отзываемого состояния.
func (s *AuthService) issue(ctx context.Context, identity *domain.Identity) (*Tokens, error) {
	identity.SessionID = uuid.NewV4().String()
	refresh := refreshTokenValue()
	if err := s.sessions.Save(ctx, refresh, identity); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}
	access, exp, err := s.issuer.IssueSession(identity.UserID, identity.Username, identity.Roles, identity.SessionID)
	if err != nil {
		_ = s.sessions.Delete(ctx, refresh)
		return nil, fmt.Errorf("issue access token: %w", err)
	}
	return &Tokens{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp, Identity: identity}, nil
}

func refreshTokenValue() string {
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	return hex.EncodeToString(secret[:])
}
