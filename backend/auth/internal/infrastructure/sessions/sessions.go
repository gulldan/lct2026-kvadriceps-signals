package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/platform/dbclient"
	dbv1 "github.com/example/auth/internal/platform/genproto/db/v1"
)

type Database interface {
	CreateAuthSession(context.Context, string, string, string, time.Duration) (*dbv1.AuthSession, error)
	RotateAuthSession(context.Context, string, string) (*dbv1.AuthSession, error)
	RevokeAuthSession(context.Context, string) error
}

// Store не сохраняет открытые секреты и не кеширует права пользователя.
type Store struct {
	db  Database
	ttl time.Duration
}

// New ограничивает абсолютный срок сессии.
func New(db Database, ttl time.Duration) *Store { return &Store{db: db, ttl: ttl} }

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (s *Store) Save(ctx context.Context, token string, identity *domain.Identity) error {
	session, err := s.db.CreateAuthSession(ctx, identity.SessionID, identity.UserID, tokenHash(token), s.ttl)
	if err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	fresh, err := fromSession(session)
	if err != nil {
		return err
	}
	*identity = *fresh
	return nil
}

// Rotate возвращает актуальные права; повтор старого секрета отзывает всю сессию.
func (s *Store) Rotate(ctx context.Context, token, next string) (*domain.Identity, error) {
	session, err := s.db.RotateAuthSession(ctx, tokenHash(token), tokenHash(next))
	if errors.Is(err, dbclient.ErrNotFound) {
		return nil, domain.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("rotate session: %w", err)
	}
	return fromSession(session)
}

func fromSession(session *dbv1.AuthSession) (*domain.Identity, error) {
	expires, err := time.Parse(time.RFC3339, session.GetExpiresAt())
	if err != nil {
		return nil, fmt.Errorf("session expiry: %w", err)
	}
	return &domain.Identity{SessionID: session.GetId(), UserID: session.GetUserId(), Username: session.GetUsername(),
		Roles: session.GetRoles(), SessionExpiresAt: expires}, nil
}

// Delete отзывает сессию по любому токену семейства.
func (s *Store) Delete(ctx context.Context, token string) error {
	if err := s.db.RevokeAuthSession(ctx, tokenHash(token)); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
