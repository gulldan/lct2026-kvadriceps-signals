// Package jwt выпускает и проверяет HS256-токены доступа; сервисы проверяют их локально общим
// секретом, поэтому горячему пути не нужен поход в auth.
package jwt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

type ctxKey struct{}

var ErrSessionUnavailable = errors.New("session verification unavailable")

type Claims struct {
	SessionID string   `json:"sid,omitempty"`
	UserID    string   `json:"uid"`
	Username  string   `json:"username"`
	Roles     []string `json:"roles"`
	gojwt.RegisteredClaims
}

type Manager struct {
	secret          []byte
	issuer          string
	accessTTL       time.Duration
	validateSession func(context.Context, *Claims) error
}

func NewManager(secret, issuer string, accessTTL time.Duration) *Manager {
	return &Manager{secret: []byte(secret), issuer: issuer, accessTTL: accessTTL}
}

func (m *Manager) Issue(userID, username string, roles []string) (string, time.Time, error) {
	return m.IssueSession(userID, username, roles, "")
}

// IssueSession связывает токен с отзываемой серверной сессией.
func (m *Manager) IssueSession(userID, username string, roles []string, sessionID string) (string, time.Time, error) {
	exp := nowUTC().Add(m.accessTTL)
	claims := Claims{
		SessionID: sessionID,
		UserID:    userID,
		Username:  username,
		Roles:     roles,
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID,
			IssuedAt:  gojwt.NewNumericDate(nowUTC()),
			ExpiresAt: gojwt.NewNumericDate(exp),
		},
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

func (m *Manager) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := gojwt.ParseWithClaims(token, claims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, gojwt.WithIssuer(m.issuer), gojwt.WithExpirationRequired(), gojwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return claims, nil
}

// SetSessionValidator вызывается до начала обслуживания запросов: поле читается без блокировки.
func (m *Manager) SetSessionValidator(validate func(context.Context, *Claims) error) {
	m.validateSession = validate
}

func (m *Manager) Validate(ctx context.Context, token string) (*Claims, error) {
	claims, err := m.Parse(token)
	if err != nil {
		return nil, err
	}
	if err := m.Revalidate(ctx, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// Revalidate переприменяет срок и отзыв сессии к уже открытым соединениям.
func (m *Manager) Revalidate(ctx context.Context, claims *Claims) error {
	if claims.ExpiresAt == nil || !time.Now().Before(claims.ExpiresAt.Time) {
		return errors.New("token expired")
	}
	if m.validateSession != nil {
		if err := m.validateSession(ctx, claims); err != nil {
			return fmt.Errorf("validate session: %w", err)
		}
	}
	return nil
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := bearer(r)
		if err != nil {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		claims, err := m.Validate(r.Context(), raw)
		if err != nil {
			if errors.Is(err, ErrSessionUnavailable) {
				http.Error(w, `{"error":"session verification unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(*Claims)
	return c, ok
}

func bearer(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errors.New("no authorization header")
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", errors.New("malformed authorization header")
	}
	return parts[1], nil
}

func nowUTC() time.Time { return time.Now().UTC() }
