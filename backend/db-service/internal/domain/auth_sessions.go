package domain

import (
	"context"
	"time"
)

type AuthSession struct {
	ID, UserID, Username string
	Roles                []string
	ExpiresAt            time.Time
}

type AuthSessions interface {
	Create(context.Context, string, string, string, time.Duration) (AuthSession, error)
	Rotate(context.Context, string, string) (AuthSession, error)
	Check(context.Context, string) (AuthSession, error)
	Revoke(context.Context, string) error
}
