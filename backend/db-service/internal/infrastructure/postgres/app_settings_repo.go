package postgres

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/jsonx"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AppSettingsRepo struct{ pool *pgxpool.Pool }

func NewAppSettingsRepo(db *DB) *AppSettingsRepo { return &AppSettingsRepo{pool: db.Pool} }

// Snapshot читает значения и ревизию одним SQL без промежуточного состояния.
func (r *AppSettingsRepo) Snapshot(ctx context.Context) (domain.AppSettingsSnapshot, error) {
	var snapshot domain.AppSettingsSnapshot
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT revision,
       COALESCE((SELECT jsonb_object_agg(key, value) FROM app_settings), '{}'::jsonb)
       FROM app_settings_revision WHERE singleton`).Scan(&snapshot.Revision, &raw)
	if err != nil {
		return snapshot, fmt.Errorf("read settings snapshot: %w", err)
	}
	if err = jsonx.Unmarshal(raw, &snapshot.Values); err != nil {
		return snapshot, fmt.Errorf("decode settings snapshot: %w", err)
	}
	return snapshot, nil
}

// Apply сериализует писателей; аудит содержит имена параметров, но не секреты.
func (r *AppSettingsRepo) Apply(ctx context.Context, expected int64, actor string, changes map[string]*string) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin settings: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM app_settings_revision WHERE singleton FOR UPDATE`).Scan(&revision); err != nil {
		return 0, fmt.Errorf("lock settings: %w", err)
	}
	if revision != expected {
		return 0, domain.ErrConflict
	}
	keys := slices.Sorted(maps.Keys(changes))
	for _, key := range keys {
		value := changes[key]
		if value == nil || *value == "" {
			_, err = tx.Exec(ctx, `DELETE FROM app_settings WHERE key=$1`, key)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value) VALUES($1,$2)
               ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=now()`, key, *value)
		}
		if err != nil {
			return 0, fmt.Errorf("write settings batch: %w", err)
		}
	}
	revision++
	if _, err = tx.Exec(ctx, `INSERT INTO app_settings_audit(revision,actor_id,changed_keys) VALUES($1,$2,$3)`,
		revision, actor, keys); err != nil {
		return 0, fmt.Errorf("audit settings: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE app_settings_revision SET revision=$1 WHERE singleton`, revision); err != nil {
		return 0, fmt.Errorf("advance settings: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit settings: %w", err)
	}
	return revision, nil
}
