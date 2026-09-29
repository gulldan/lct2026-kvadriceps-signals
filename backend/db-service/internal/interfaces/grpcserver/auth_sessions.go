package grpcserver

import (
	"context"
	"encoding/hex"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthSessions struct {
	dbv1.UnimplementedAuthSessionsServer
	repo domain.AuthSessions
}

func NewAuthSessions(repo domain.AuthSessions) *AuthSessions { return &AuthSessions{repo: repo} }

func validRefreshHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == hash
}

func sessionPB(session domain.AuthSession) *dbv1.AuthSession {
	return &dbv1.AuthSession{Id: session.ID, UserId: session.UserID, Username: session.Username,
		Roles: session.Roles, ExpiresAt: session.ExpiresAt.Format(time.RFC3339)}
}

// Create сохраняет только хеш секретного токена.
func (s *AuthSessions) Create(ctx context.Context, req *dbv1.CreateAuthSessionRequest) (*dbv1.AuthSession, error) {
	_, idErr := uuid.Parse(req.GetId())
	_, userErr := uuid.Parse(req.GetUserId())
	if idErr != nil ||
		userErr != nil ||
		!validRefreshHash(
			req.GetRefreshHash(),
		) ||
		req.GetTtlSeconds() < 60 ||
		req.GetTtlSeconds() > 2592000 {
		return nil, status.Error(codes.InvalidArgument, "invalid session")
	}
	session, err := s.repo.Create(
		ctx,
		req.GetId(),
		req.GetUserId(),
		req.GetRefreshHash(),
		time.Duration(
			req.GetTtlSeconds(),
		)*time.Second,
	)
	if err != nil {
		return nil, toStatus(err)
	}
	return sessionPB(session), nil
}

func (s *AuthSessions) Rotate(ctx context.Context, req *dbv1.RotateAuthSessionRequest) (*dbv1.AuthSession, error) {
	if !validRefreshHash(req.GetRefreshHash()) || !validRefreshHash(req.GetNextHash()) || req.GetRefreshHash() == req.GetNextHash() {
		return nil, status.Error(codes.InvalidArgument, "invalid rotation")
	}
	session, err := s.repo.Rotate(ctx, req.GetRefreshHash(), req.GetNextHash())
	if err != nil {
		return nil, toStatus(err)
	}
	return sessionPB(session), nil
}

func (s *AuthSessions) Check(ctx context.Context, req *dbv1.CheckAuthSessionRequest) (*dbv1.AuthSession, error) {
	if _, err := uuid.Parse(req.GetId()); err != nil {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	session, err := s.repo.Check(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return sessionPB(session), nil
}

func (s *AuthSessions) Revoke(ctx context.Context, req *dbv1.RevokeAuthSessionRequest) (*dbv1.RevokeAuthSessionResponse, error) {
	if !validRefreshHash(req.GetRefreshHash()) {
		return nil, status.Error(codes.InvalidArgument, "invalid refresh hash")
	}
	if err := s.repo.Revoke(ctx, req.GetRefreshHash()); err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.RevokeAuthSessionResponse{}, nil
}
