package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/jwt"
)

func sessionValidator(db *dbclient.Client) func(context.Context, *jwt.Claims) error {
	return func(ctx context.Context, claims *jwt.Claims) error {
		if claims.SessionID == "" {
			return errors.New("session is required")
		}
		session, err := db.CheckAuthSession(ctx, claims.SessionID)
		if errors.Is(err, dbclient.ErrNotFound) {
			return errors.New("session revoked or expired")
		}
		if err != nil {
			return fmt.Errorf("%w: %w", jwt.ErrSessionUnavailable, err)
		}
		if session.GetUserId() != claims.UserID {
			return errors.New("session identity mismatch")
		}
		claims.Roles = session.GetRoles()
		claims.Username = session.GetUsername()
		return nil
	}
}
