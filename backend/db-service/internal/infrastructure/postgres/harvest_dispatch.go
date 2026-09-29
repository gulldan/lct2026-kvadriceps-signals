package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/example/db-service/internal/platform/harvest"
)

func (r *HarvestRepo) Pending(ctx context.Context, in harvest.Pending) (harvest.PendingResult, error) {
	var result harvest.PendingResult
	if in.Limit < 1 || in.Limit > 10 {
		return result, harvest.ErrInvalid
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return result, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockHarvestLease(ctx, tx, in.Lease); err != nil {
		return result, collectionStorageError(err)
	}
	rows, err := tx.Query(ctx, `
 SELECT payload FROM source_collection_dispatch WHERE scope_id=$1 AND dispatched_at IS NULL ORDER BY
created_at, document_id LIMIT $2
`, in.Lease.ScopeID, in.Limit)
	if err != nil {
		return result, collectionStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return result, collectionStorageError(err)
		}
		var record harvest.Dispatch
		if err = json.Unmarshal(raw, &record, json.RejectUnknownMembers(true)); err != nil {
			return result, collectionStorageError(err)
		}
		result.Records = append(result.Records, record)
	}
	if err = rows.Err(); err != nil {
		return result, collectionStorageError(err)
	}
	return result, collectionStorageError(tx.Commit(ctx))
}

func (r *HarvestRepo) Ack(ctx context.Context, in harvest.Ack) (harvest.PendingResult, error) {
	var result harvest.PendingResult
	if _, err := uuid.Parse(in.DocumentID); err != nil {
		return result, harvest.ErrInvalid
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return result, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockHarvestLease(ctx, tx, in.Lease); err != nil {
		return result, collectionStorageError(err)
	}
	tag, err := tx.Exec(ctx, `
 UPDATE source_collection_dispatch SET dispatched_at=coalesce(dispatched_at, now()) WHERE scope_id=$1 AND
document_id=$2
`, in.Lease.ScopeID, in.DocumentID)
	if err != nil {
		return result, collectionStorageError(err)
	}
	if tag.RowsAffected() != 1 {
		return result, harvest.ErrConflict
	}
	return result, collectionStorageError(tx.Commit(ctx))
}

func (r *HarvestRepo) Finish(ctx context.Context, in harvest.Finish) (harvest.Job, error) {
	if !slices.Contains([]string{statusComplete, "partial", jobFailedState}, in.Status) || len(in.Error) > 1024 ||
		(in.ResetCursor && in.Status != jobFailedState) {
		return harvest.Job{}, harvest.ErrInvalid
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	j, err := lockHarvestLease(ctx, tx, in.Lease)
	if errors.Is(err, harvest.ErrConflict) {
		// Потерянный ответ Finish повторяется с тем же токеном и результатом.
		prior, readErr := readHarvestJob(ctx, tx, in.Lease.ScopeID)
		if readErr == nil && prior.Lease == in.Lease && prior.Status == in.Status {
			return prior, collectionStorageError(tx.Commit(ctx))
		}
	}
	if err != nil {
		return j, collectionStorageError(err)
	}
	if in.Status == "complete" {
		var pending bool
		if err = tx.QueryRow(ctx, `
 SELECT EXISTS(SELECT 1 FROM source_collection_dispatch WHERE scope_id=$1 AND dispatched_at IS NULL)
`, in.Lease.ScopeID).Scan(&pending); err != nil {
			return j, collectionStorageError(err)
		}
		if !j.Exhausted || pending {
			return j, fmt.Errorf("%w: unfinished source or dispatch", harvest.ErrConflict)
		}
	}
	_, err = tx.Exec(ctx, `
 UPDATE source_collection_scopes SET status=$2, last_error=$3, last_success_at=CASE WHEN $2='complete'
AND NOT incomplete THEN now() ELSE last_success_at END, updated_at=now(),
cursor=CASE WHEN $4 THEN '' ELSE cursor END,
exhausted=CASE WHEN $4 THEN false ELSE exhausted END,
incomplete=CASE WHEN $4 THEN false ELSE incomplete END WHERE id=$1
`, in.Lease.ScopeID, in.Status, in.Error, in.ResetCursor)
	if err != nil {
		return j, collectionStorageError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE source_collection_limits SET leased_until='-infinity',lease_token='' WHERE provider=$1`,
		j.Provider,
	)
	if err != nil {
		return j, collectionStorageError(err)
	}
	j.Status = in.Status
	if in.ResetCursor {
		j.Cursor = ""
		j.Exhausted = false
		j.Incomplete = false
	}
	return j, collectionStorageError(tx.Commit(ctx))
}
