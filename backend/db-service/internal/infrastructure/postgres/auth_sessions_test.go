package postgres_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestSessionsRotateRevokeRefreshRolesAndToleratePageRaces(t *testing.T) {
	db := newTestDB(t)
	user := &domain.User{ID: uuid.NewV4().String(), Username: uuid.NewV4().String(),
		PasswordHash: "fixture", Roles: []string{"admin"}, CreatedAt: time.Now()}
	if err := postgres.NewUserRepo(db).Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewAuthSessionsRepo(db)
	hash := func() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewV4().String()))) }
	old, next := hash(), hash()
	id := uuid.NewV4().String()
	original, err := repo.Create(t.Context(), id, user.ID, old, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(t.Context(), `UPDATE users SET roles=ARRAY['user'] WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	checked, err := repo.Check(t.Context(), id)
	if err != nil || len(checked.Roles) != 1 || checked.Roles[0] != "user" {
		t.Fatalf("stale privileges: %+v %v", checked, err)
	}
	rotated, err := repo.Rotate(t.Context(), old, next)
	if err != nil || !rotated.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatalf("rotation: %+v %v", rotated, err)
	}
	if _, err = repo.Rotate(t.Context(), old, hash()); err != nil {
		t.Fatalf("page race revoked the session: %v", err)
	}
	if _, err = db.Pool.Exec(
		t.Context(),
		`UPDATE auth_refresh_tokens SET consumed_at=now()-interval '1 minute' WHERE hash=$1`,
		old,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Rotate(t.Context(), old, hash()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("replay accepted: %v", err)
	}
	if _, err = repo.Check(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("replayed session live: %v", err)
	}
	if _, err = repo.Rotate(t.Context(), next, hash()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("descendant live: %v", err)
	}

	id, old = uuid.NewV4().String(), hash()
	if _, err = repo.Create(t.Context(), id, user.ID, old, time.Hour); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var writers sync.WaitGroup
	for range 2 {
		writers.Go(func() {
			_, stepErr := repo.Rotate(t.Context(), old, hash())
			if stepErr == nil {
				wins.Add(1)
			} else if !errors.Is(stepErr, domain.ErrNotFound) {
				t.Error(stepErr)
			}
		})
	}
	writers.Wait()
	if wins.Load() != 2 {
		t.Fatalf("concurrent tabs lost the session: %d winners", wins.Load())
	}
	if _, err = repo.Check(t.Context(), id); err != nil {
		t.Fatalf("concurrent tabs revoked the session: %v", err)
	}

	id, old = uuid.NewV4().String(), hash()
	if _, err = repo.Create(t.Context(), id, user.ID, old, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = repo.Revoke(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Check(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("logout left session live: %v", err)
	}
	id, old = uuid.NewV4().String(), hash()
	if _, err = repo.Create(t.Context(), id, user.ID, old, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(
		t.Context(),
		`UPDATE auth_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`,
		id,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Rotate(t.Context(), old, hash()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired refresh accepted: %v", err)
	}
}
