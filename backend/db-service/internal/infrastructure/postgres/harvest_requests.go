package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/example/db-service/internal/platform/harvest"
	"github.com/jackc/pgx/v5"
)

// Request идемпотентен по request_id; другой вход с тем же id — конфликт.
func (r *HarvestRepo) Request(ctx context.Context, in harvest.Request) (harvest.Collection, error) {
	if err := in.Validate(r.panel); err != nil {
		return harvest.Collection{}, err
	}
	scope, err := json.Marshal(in.Scope)
	if err != nil {
		return harvest.Collection{}, collectionStorageError(err)
	}
	urls, err := json.Marshal(in.URLs)
	if err != nil {
		return harvest.Collection{}, collectionStorageError(err)
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return harvest.Collection{}, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existingOwner, existingFingerprint string
	id := in.ScopeID()
	err = tx.QueryRow(ctx, `SELECT owner_id, request_fingerprint FROM source_collection_scopes
WHERE id=$1 FOR UPDATE`, id).Scan(&existingOwner, &existingFingerprint)
	switch {
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return harvest.Collection{}, collectionStorageError(err)
	case err == nil && existingOwner != in.OwnerID:
		return harvest.Collection{}, harvest.ErrConflict
	case err == nil && existingFingerprint != "" && existingFingerprint != in.Fingerprint():
		return harvest.Collection{}, harvest.ErrConflict
	case errors.Is(err, pgx.ErrNoRows):
		_, err = tx.Exec(ctx, `INSERT INTO source_collection_scopes
(id, owner_id, provider, scope, run_id, lease_token, run_limit, status, test_run_id,
 request_id, request_fingerprint, request_urls, requested_at)
VALUES($1,$2,$3,$4,'','',$5,'requested',$6,$7,$8,$9,now())`,
			id, in.OwnerID, in.Provider, scope, in.Limit, in.TestRunID, in.RequestID, in.Fingerprint(), urls)
		if err != nil {
			return harvest.Collection{}, collectionStorageError(err)
		}
	default:
		// Сбор уже есть: запоминаем повторный запрос, чтобы и под новым request_id
		// он оставался идемпотентным по охвату.
		if _, err = tx.Exec(ctx, `UPDATE source_collection_scopes
SET request_id=$2, request_fingerprint=$3, request_urls=$4, requested_at=coalesce(requested_at, now())
WHERE id=$1 AND request_id=''`, id, in.RequestID, in.Fingerprint(), urls); err != nil {
			return harvest.Collection{}, collectionStorageError(err)
		}
	}
	collection, err := readCollection(ctx, tx, id, in.OwnerID)
	if err != nil {
		return harvest.Collection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return harvest.Collection{}, collectionStorageError(err)
	}
	return collection, nil
}

// Progress отвечает на чужой сбор «не найден», а не «пусто».
func (r *HarvestRepo) Progress(ctx context.Context, in harvest.Progress) (harvest.Collection, error) {
	if in.OwnerID == "" || in.ScopeID == "" {
		return harvest.Collection{}, harvest.ErrInvalid
	}
	return readCollection(ctx, r.db.Pool, in.ScopeID, in.OwnerID)
}

func (r *HarvestRepo) Requested(ctx context.Context, in harvest.Requested) ([]harvest.Collection, error) {
	if in.Provider == "" || in.Limit < 1 || in.Limit > 100 {
		return nil, harvest.ErrInvalid
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT id, owner_id FROM source_collection_scopes
WHERE provider=$1 AND status='requested' AND request_id<>'' ORDER BY requested_at, id LIMIT $2`,
		in.Provider, in.Limit)
	if err != nil {
		return nil, collectionStorageError(err)
	}
	defer rows.Close()
	ids := [][2]string{}
	for rows.Next() {
		var id, owner string
		if err = rows.Scan(&id, &owner); err != nil {
			return nil, collectionStorageError(err)
		}
		ids = append(ids, [2]string{id, owner})
	}
	if err = rows.Err(); err != nil {
		return nil, collectionStorageError(err)
	}
	out := make([]harvest.Collection, 0, len(ids))
	for _, pair := range ids {
		collection, readErr := readCollection(ctx, r.db.Pool, pair[0], pair[1])
		if readErr != nil {
			return nil, readErr
		}
		out = append(out, collection)
	}
	return out, nil
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readCollection(ctx context.Context, q queryer, id, owner string) (harvest.Collection, error) {
	var c harvest.Collection
	var scope, urls []byte
	var requestedAt *time.Time
	err := q.QueryRow(ctx, `SELECT id, owner_id, request_id, provider, scope::text, request_urls::text,
run_limit, status, cursor, received, last_error, requested_at, updated_at
FROM source_collection_scopes WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&c.ScopeID, &c.OwnerID,
		&c.RequestID, &c.Provider, &scope, &urls, &c.Limit, &c.State, &c.Cursor, &c.Received,
		&c.LastError, &requestedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, harvest.ErrNotFound
	}
	if err != nil {
		return c, collectionStorageError(err)
	}
	if requestedAt != nil {
		c.RequestedAt = *requestedAt
	}
	if err = json.Unmarshal(scope, &c.Scope); err != nil {
		return c, collectionStorageError(err)
	}
	if err = json.Unmarshal(urls, &c.URLs); err != nil {
		return c, collectionStorageError(err)
	}
	c.Manifests, err = readCollectionManifests(ctx, q, id)
	return c, err
}

func readCollectionManifests(ctx context.Context, q queryer, id string) ([]harvest.Manifest, error) {
	rows, err := q.Query(ctx, `SELECT run_id, pages, listed, received, rejected, processed, cursor,
exhausted, incomplete, completeness, limitations::text, started_at, updated_at
FROM collection_manifests WHERE scope_id=$1 ORDER BY started_at, run_id`, id)
	if err != nil {
		return nil, collectionStorageError(err)
	}
	defer rows.Close()
	out := []harvest.Manifest{}
	for rows.Next() {
		var m harvest.Manifest
		var limitations []byte
		if err = rows.Scan(&m.RunID, &m.Pages, &m.Listed, &m.Received, &m.Rejected, &m.Processed, &m.Cursor,
			&m.Exhausted, &m.Incomplete, &m.Completeness, &limitations, &m.StartedAt, &m.UpdatedAt); err != nil {
			return nil, collectionStorageError(err)
		}
		if err = json.Unmarshal(limitations, &m.Limitations); err != nil {
			return nil, fmt.Errorf("read collection limitations: %w", err)
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, collectionStorageError(err)
	}
	return out, nil
}
