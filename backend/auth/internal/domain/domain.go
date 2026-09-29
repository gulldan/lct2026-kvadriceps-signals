package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidCredentials одна на неизвестный логин и неверный пароль, чтобы ответ не выдавал,
// какие логины существуют.
var ErrInvalidCredentials = errors.New("invalid credentials")

var ErrSessionNotFound = errors.New("session not found")

var ErrUserExists = errors.New("username already taken")

var ErrWeakPassword = errors.New("password does not meet requirements")

var ErrTooManyAttempts = errors.New("too many failed login attempts")

type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string {
	return fmt.Sprintf("%s: retry after %s", ErrTooManyAttempts.Error(), e.RetryAfter)
}

func (e *TooManyAttemptsError) Unwrap() error { return ErrTooManyAttempts }

type Identity struct {
	SessionID        string    `json:"session_id"`
	SessionExpiresAt time.Time `json:"session_expires_at"`
	UserID           string    `json:"user_id"`
	Username         string    `json:"username"`
	Roles            []string  `json:"roles"`
}

// UserDirectory: bcrypt сравнивает адаптер, поэтому слой приложения не видит хеш пароля.
type UserDirectory interface {
	Authenticate(ctx context.Context, username, password string) (*Identity, error)
	Register(ctx context.Context, username, password string, roles []string) (*Identity, error)
}

// LoginThrottle защищает от перебора и от нагрузки bcrypt на CPU. Недоступное хранилище
// не должно блокировать вход — реализации в этом случае пропускают.
type LoginThrottle interface {
	Allowed(ctx context.Context, key string) (ok bool, retryAfter time.Duration, err error)
	RecordFailure(ctx context.Context, key string) error
	Reset(ctx context.Context, key string) error
}

type SessionStore interface {
	Save(ctx context.Context, token string, identity *Identity) error
	// Rotate однократно заменяет токен и возвращает актуальную личность.
	Rotate(ctx context.Context, token, next string) (*Identity, error)
	// Delete отсутствующего токена ничего не делает: выход идемпотентен.
	Delete(ctx context.Context, token string) error
}
