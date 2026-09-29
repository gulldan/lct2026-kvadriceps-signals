package sessions_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/infrastructure/sessions"
	dbv1 "github.com/example/auth/internal/platform/genproto/db/v1"
)

type database struct{ hashes []string }

func (d *database) CreateAuthSession(_ context.Context, id, user, hash string, _ time.Duration) (*dbv1.AuthSession, error) {
	d.hashes = append(d.hashes, hash)
	return &dbv1.AuthSession{
		Id:       id,
		UserId:   user,
		Username: "reader",
		Roles: []string{
			"user",
		},
		ExpiresAt: time.Now().Add(
			time.Hour,
		).Format(
			time.RFC3339,
		),
	}, nil
}
func (d *database) RotateAuthSession(ctx context.Context, old, next string) (*dbv1.AuthSession, error) {
	d.hashes = append(d.hashes, old)
	return d.CreateAuthSession(ctx, "session", "user", next, time.Hour)
}
func (d *database) RevokeAuthSession(_ context.Context, hash string) error {
	d.hashes = append(d.hashes, hash)
	return nil
}

func TestStoreNeverSendsRefreshSecrets(t *testing.T) {
	db := &database{}
	store := sessions.New(db, time.Hour)
	identity := &domain.Identity{SessionID: "session", UserID: "user"}
	if err := store.Save(t.Context(), "first-secret", identity); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.Rotate(t.Context(), "first-secret", "next-secret")
	if err != nil || fresh.SessionID != "session" || fresh.SessionExpiresAt.IsZero() {
		t.Fatalf("rotate: %+v %v", fresh, err)
	}
	if err := store.Delete(t.Context(), "next-secret"); err != nil {
		t.Fatal(err)
	}
	for i, secret := range []string{"first-secret", "first-secret", "next-secret", "next-secret"} {
		expected := fmt.Sprintf("%x", sha256.Sum256([]byte(secret)))
		if db.hashes[i] != expected {
			t.Fatalf("unsafe payload %d", i)
		}
	}
}
