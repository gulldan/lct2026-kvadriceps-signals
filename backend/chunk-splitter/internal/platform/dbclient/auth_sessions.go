package dbclient

import (
	"context"
	"time"

	dbv1 "github.com/example/chunk-splitter/internal/platform/genproto/db/v1"
)

// CreateAuthSession открывает сессию; токены передаются только в виде хешей.
func (c *Client) CreateAuthSession(ctx context.Context, id, user, hash string, ttl time.Duration) (*dbv1.AuthSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := dbv1.NewAuthSessionsClient(c.conn).Create(ctx, &dbv1.CreateAuthSessionRequest{
		Id: id, UserId: user, RefreshHash: hash, TtlSeconds: int64(ttl / time.Second),
	})
	return r, mapErr(err)
}

// RotateAuthSession потребляет старый токен и выдаёт единственного преемника.
func (c *Client) RotateAuthSession(ctx context.Context, hash, next string) (*dbv1.AuthSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := dbv1.NewAuthSessionsClient(c.conn).Rotate(ctx, &dbv1.RotateAuthSessionRequest{RefreshHash: hash, NextHash: next})
	return r, mapErr(err)
}

// CheckAuthSession проверяет отзыв и актуальные права без локального кэша.
func (c *Client) CheckAuthSession(ctx context.Context, id string) (*dbv1.AuthSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := dbv1.NewAuthSessionsClient(c.conn).Check(ctx, &dbv1.CheckAuthSessionRequest{Id: id})
	return r, mapErr(err)
}

// RevokeAuthSession отзывает все поколения токенов данной сессии.
func (c *Client) RevokeAuthSession(ctx context.Context, hash string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := dbv1.NewAuthSessionsClient(c.conn).Revoke(ctx, &dbv1.RevokeAuthSessionRequest{RefreshHash: hash})
	return mapErr(err)
}
