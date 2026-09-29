package postgres_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestSettingsBatchRollbackCASAndAudit(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewAppSettingsRepo(db)
	before, err := repo.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	key := "TEST_" + uuid.NewV4().String()
	changes := map[string]*string{key: new("secret-value"), key + "_MODEL": new("model")}
	if _, stepErr := repo.Apply(t.Context(), before.Revision, "invalid-uuid", changes); stepErr == nil {
		t.Fatal("invalid audit accepted")
	}
	after, err := repo.Snapshot(t.Context())
	if err != nil || after.Revision != before.Revision || after.Values[key] != "" {
		t.Fatalf("partial settings: %+v %v", after, err)
	}
	var won, lost atomic.Int32
	var writers sync.WaitGroup
	actor := uuid.NewV4().String()
	for range 2 {
		writers.Go(func() {
			_, stepErr := repo.Apply(t.Context(), before.Revision, actor, changes)
			switch {
			case stepErr == nil:
				won.Add(1)
			case errors.Is(stepErr, domain.ErrConflict):
				lost.Add(1)
			default:
				t.Error(stepErr)
			}
		})
	}
	writers.Wait()
	if won.Load() != 1 || lost.Load() != 1 {
		t.Fatalf("writers: %d/%d", won.Load(), lost.Load())
	}
	after, err = repo.Snapshot(t.Context())
	if err != nil ||
		after.Revision != before.Revision+1 ||
		after.Values[key] != "secret-value" ||
		after.Values[key+"_MODEL"] != "model" {
		t.Fatalf("incoherent snapshot: %+v %v", after, err)
	}
	var savedActor string
	var keys []string
	if err = db.Pool.QueryRow(
		t.Context(),
		`SELECT actor_id, changed_keys FROM app_settings_audit WHERE revision=$1`,
		after.Revision,
	).Scan(
		&savedActor,
		&keys,
	); err != nil {
		t.Fatal(err)
	}
	if savedActor != actor || len(keys) != 2 || keys[0] != key || keys[1] != key+"_MODEL" {
		t.Fatalf("audit: %s %v", savedActor, keys)
	}
}
