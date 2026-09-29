package directory

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/platform/dbclient"

	commonv1 "github.com/example/auth/internal/platform/genproto/common/v1"
)

// mustDummyHash: неизвестный логин тоже проходит bcrypt той же цены, чтобы время ответа не выдавало
// существующие учётки. Хеш генерируется, а не зашит: на битом хеше сравнение завершается мгновенно.
func mustDummyHash() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte(uuid.NewV4().String()), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("directory: generate dummy bcrypt hash: %v", err))
	}
	return h
}

type Directory struct {
	db        *dbclient.Client
	timeout   time.Duration
	dummyHash []byte
}

// New: timeout ≤ 0 отключает дедлайн вызовов db-service.
func New(db *dbclient.Client, timeout time.Duration) *Directory {
	return &Directory{db: db, timeout: timeout, dummyHash: mustDummyHash()}
}

func (d *Directory) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if d.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d.timeout)
}

func (d *Directory) Authenticate(ctx context.Context, username, password string) (*domain.Identity, error) {
	dbCtx, cancel := d.withTimeout(ctx)
	defer cancel()

	user, err := d.db.GetUserByUsername(dbCtx, username)
	if err != nil {
		if errors.Is(err, dbclient.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword(d.dummyHash, []byte(password))
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.GetPasswordHash()), []byte(password)); err != nil {
		return nil, domain.ErrInvalidCredentials
	}
	return identityFromUser(user), nil
}

func (d *Directory) Register(ctx context.Context, username, password string, roles []string) (*domain.Identity, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	dbCtx, cancel := d.withTimeout(ctx)
	defer cancel()

	user, err := d.db.CreateUser(dbCtx, username, string(hash), roles)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrUserExists
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return identityFromUser(user), nil
}

// Типизированный статус сохраняет смысл конфликта, не раскрывая SQL клиенту.
func isUniqueViolation(err error) bool { return status.Code(err) == codes.AlreadyExists }

// identityFromUser намеренно не переносит хеш пароля.
func identityFromUser(u *commonv1.User) *domain.Identity {
	return &domain.Identity{
		UserID:   u.GetId(),
		Username: u.GetUsername(),
		Roles:    u.GetRoles(),
	}
}
