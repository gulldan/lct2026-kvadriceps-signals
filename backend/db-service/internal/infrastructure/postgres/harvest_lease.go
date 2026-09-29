package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/example/db-service/internal/platform/harvest"
	"github.com/jackc/pgx/v5"
)

// HarvestRepo держит контрольные точки и отправку в одной транзакции.
type HarvestRepo struct {
	db    *DB
	panel []string
}

func NewHarvestRepo(db *DB) *HarvestRepo { return &HarvestRepo{db: db} }

// WithPanel задаёт домены, которые может назвать запрошенный веб-сбор.
func (r *HarvestRepo) WithPanel(domains []string) *HarvestRepo {
	r.panel = slices.Clone(domains)
	return r
}

func (r *HarvestRepo) Start(ctx context.Context, in harvest.Start) (harvest.Job, error) {
	if err := in.Validate(); err != nil {
		return harvest.Job{}, fmt.Errorf("%w: %w", harvest.ErrInvalid, err)
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO source_collection_limits(provider) VALUES($1) ON CONFLICT DO NOTHING`,
		in.Provider,
	); err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	var active bool
	var heldScope, heldToken string
	err = tx.QueryRow(
		ctx,
		`
 SELECT leased_until > clock_timestamp(), scope_id, lease_token FROM source_collection_limits WHERE
provider=$1 FOR UPDATE
`,
		in.Provider,
	).Scan(
		&active,
		&heldScope,
		&heldToken,
	)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	id := in.ScopeID()
	if active && (heldScope != id || heldToken != in.Token) {
		return harvest.Job{}, harvest.ErrConflict
	}
	job, err := readHarvestJob(ctx, tx, id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return harvest.Job{}, collectionStorageError(err)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		scope, e := json.Marshal(in.Scope)
		if e != nil {
			return harvest.Job{}, collectionStorageError(e)
		}
		_, err = tx.Exec(
			ctx,
			`
 INSERT INTO source_collection_scopes(id, owner_id, provider, scope, run_id, lease_token, run_limit,
status, test_run_id) VALUES($1, $2, $3, $4, $5, $6, $7, 'running', $8)
`,
			id,
			in.OwnerID,
			in.Provider,
			scope,
			in.RunID,
			in.Token,
			in.Limit,
			in.TestRunID,
		)
	case job.Lease.Token != in.Token:
		// Ограниченная пачка продолжает незавершённый курсор; законченный проход начинается заново
		// с начала фиксированного охвата, давая полное перекрытие.
		_, err = tx.Exec(
			ctx,
			`
 UPDATE source_collection_scopes SET cursor=CASE WHEN status='complete' THEN '' ELSE cursor END,
exhausted=CASE WHEN status='complete' THEN false ELSE exhausted END, incomplete=CASE WHEN
status='complete' THEN false ELSE incomplete END, run_id=$2, lease_token=$3, run_limit=$4, received=0,
page_number=0, status='running', last_error='', updated_at=now() WHERE id=$1
`,
			id,
			in.RunID,
			in.Token,
			in.Limit,
		)
	case job.Lease.RunID != in.RunID || job.Status != "running":
		return harvest.Job{}, harvest.ErrConflict
	}
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	_, err = tx.Exec(
		ctx,
		`
 UPDATE source_collection_limits SET scope_id=$2, lease_token=$3, leased_until=clock_timestamp()+interval
'2 minutes' WHERE provider=$1
`,
		in.Provider,
		id,
		in.Token,
	)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	job, err = readHarvestJob(ctx, tx, id)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	return job, collectionStorageError(tx.Commit(ctx))
}

func readHarvestJob(ctx context.Context, tx pgx.Tx, id string) (harvest.Job, error) {
	var j harvest.Job
	var raw []byte
	err := tx.QueryRow(
		ctx,
		`
 SELECT id, run_id, lease_token, owner_id, provider, scope, cursor, received, run_limit, status,
test_run_id, page_number, exhausted, incomplete FROM source_collection_scopes WHERE id=$1 FOR UPDATE
`,
		id,
	).Scan(
		&j.Lease.ScopeID,
		&j.Lease.RunID,
		&j.Lease.Token,
		&j.OwnerID,
		&j.Provider,
		&raw,
		&j.Cursor,
		&j.Received,
		&j.Limit,
		&j.Status,
		&j.TestRunID,
		&j.PageNumber,
		&j.Exhausted,
		&j.Incomplete,
	)
	if err != nil {
		return j, collectionStorageError(err)
	}
	err = json.Unmarshal(raw, &j.Scope, json.RejectUnknownMembers(true))
	return j, collectionStorageError(err)
}

func lockHarvestLease(ctx context.Context, tx pgx.Tx, lease harvest.Lease) (harvest.Job, error) {
	if err := lease.Validate(); err != nil {
		return harvest.Job{}, fmt.Errorf("%w: %w", harvest.ErrInvalid, err)
	}
	var provider string
	if err := tx.QueryRow(ctx, `SELECT provider FROM source_collection_scopes WHERE id=$1`, lease.ScopeID).Scan(
		&provider,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return harvest.Job{}, harvest.ErrConflict
		}
		return harvest.Job{}, collectionStorageError(err)
	}
	var valid bool
	err := tx.QueryRow(
		ctx,
		`
 SELECT scope_id=$2 AND lease_token=$3 AND leased_until>clock_timestamp() FROM source_collection_limits
WHERE provider=$1 FOR UPDATE
`,
		provider,
		lease.ScopeID,
		lease.Token,
	).Scan(
		&valid,
	)
	if err != nil {
		return harvest.Job{}, collectionStorageError(err)
	}
	if !valid {
		return harvest.Job{}, harvest.ErrConflict
	}
	j, err := readHarvestJob(ctx, tx, lease.ScopeID)
	if err != nil {
		return j, collectionStorageError(err)
	}
	if j.Lease != lease || j.Status != "running" {
		return j, harvest.ErrConflict
	}
	_, err = tx.Exec(
		ctx,
		`
 UPDATE source_collection_limits SET leased_until=clock_timestamp()+interval '2 minutes' WHERE
provider=$1
`,
		provider,
	)
	return j, collectionStorageError(err)
}

func (r *HarvestRepo) Quota(ctx context.Context, in harvest.Quota) (harvest.Grant, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return harvest.Grant{}, collectionStorageError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	j, err := lockHarvestLease(ctx, tx, in.Lease)
	if err != nil {
		return harvest.Grant{}, collectionStorageError(err)
	}
	var now, next time.Time
	if err = tx.QueryRow(
		ctx,
		`SELECT clock_timestamp(),next_request_at FROM source_collection_limits WHERE provider=$1`,
		j.Provider,
	).Scan(
		&now,
		&next,
	); err != nil {
		return harvest.Grant{}, collectionStorageError(err)
	}
	if in.NotBefore != nil && in.NotBefore.After(next) {
		next = *in.NotBefore
	}
	grant := harvest.Grant{Allowed: in.Reserve && !next.After(now), NotBefore: next}
	if grant.Allowed {
		interval := time.Second
		switch j.Provider {
		case "arxiv":
			interval = 3 * time.Second
		case "github":
			interval = 6 * time.Second
		}
		next = now.Add(interval)
		grant.NotBefore = next
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE source_collection_limits SET next_request_at=$2 WHERE provider=$1`,
		j.Provider,
		next,
	)
	if err != nil {
		return harvest.Grant{}, collectionStorageError(err)
	}
	return grant, collectionStorageError(tx.Commit(ctx))
}

func collectionStorageError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("collection storage: %w", err)
}
