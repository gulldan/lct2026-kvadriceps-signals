package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const jobFailedState = "failed"

type HypothesisJobsRepo struct{ pool *pgxpool.Pool }

func NewHypothesisJobsRepo(db *DB) *HypothesisJobsRepo { return &HypothesisJobsRepo{pool: db.Pool} }

const hypothesisJobColumns = `id,owner_id,kind,input,state,result_ids,error,created_at,started_at,heartbeat_at,
 finished_at,attempt,token`

func scanHypothesisJob(row pgx.Row) (*contracts.HypothesisJob, error) {
	j := new(contracts.HypothesisJob)
	var raw []byte
	var created time.Time
	var started, heartbeat, finished pgtype.Timestamptz
	err := row.Scan(
		&j.ID,
		&j.OwnerID,
		&j.Kind,
		&raw,
		&j.Status,
		&j.ResultIDs,
		&j.Error,
		&created,
		&started,
		&heartbeat,
		&finished,
		&j.Attempt,
		&j.Token,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan hypothesis job: %w", err)
	}
	if err = json.Unmarshal(raw, &j.Input); err != nil {
		return nil, fmt.Errorf("scan hypothesis job: %w", err)
	}
	j.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	if started.Valid {
		j.StartedAt = started.Time.UTC().Format(time.RFC3339Nano)
	}
	if heartbeat.Valid {
		j.HeartbeatAt = heartbeat.Time.UTC().Format(time.RFC3339Nano)
	}
	if finished.Valid {
		j.FinishedAt = finished.Time.UTC().Format(time.RFC3339Nano)
	}
	return j, nil
}

// EnqueueHypothesisJob ставит работу атомарно; повтор того же намерения возвращает запись.
func (r *HypothesisJobsRepo) EnqueueHypothesisJob(
	ctx context.Context,
	in *contracts.HypothesisJob,
) (*contracts.HypothesisJob, error) {
	if in.ID == "" ||
		len(
			in.ID,
		) > 128 ||
		in.OwnerID == "" ||
		!slices.Contains(
			[]string{
				"generate",
				"verify",
				"enrich",
				"assess_trl",
				"competitors",
				"refine",
				"tag",
			},
			in.Kind,
		) {
		return nil, domain.ErrInvalidArgument
	}
	raw, err := json.Marshal(in.Input)
	if err != nil || len(raw) > 65536 {
		return nil, domain.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("enqueue hypothesis job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "hypothesis-jobs/"+in.OwnerID); err != nil {
		return nil, fmt.Errorf("enqueue hypothesis job: %w", err)
	}
	existing, err := scanHypothesisJob(tx.QueryRow(ctx, `SELECT `+hypothesisJobColumns+` FROM hypothesis_jobs WHERE id=$1`, in.ID))
	if err == nil {
		previous, encodeErr := json.Marshal(existing.Input)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode existing job: %w", encodeErr)
		}
		if existing.OwnerID != in.OwnerID || existing.Kind != in.Kind || string(previous) != string(raw) {
			return nil, domain.ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	var queued int
	if err = tx.QueryRow(
		ctx,
		`SELECT count(*) FROM hypothesis_jobs WHERE owner_id=$1 AND state IN ('queued','running')`,
		in.OwnerID,
	).Scan(
		&queued,
	); err != nil {
		return nil, fmt.Errorf("enqueue hypothesis job: %w", err)
	}
	if queued >= 100 {
		return nil, domain.ErrConflict
	}
	out, err := scanHypothesisJob(
		tx.QueryRow(
			ctx,
			`INSERT INTO hypothesis_jobs(id,owner_id,kind,input,state) VALUES($1,$2,$3,$4,'queued') RETURNING `+hypothesisJobColumns,
			in.ID,
			in.OwnerID,
			in.Kind,
			raw,
		),
	)
	if err != nil {
		return nil, err
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return out, fmt.Errorf("enqueue hypothesis job: %w", operationErr)
	}
	return out, nil
}

func (r *HypothesisJobsRepo) GetHypothesisJob(ctx context.Context, owner, id string) (*contracts.HypothesisJob, error) {
	return scanHypothesisJob(
		r.pool.QueryRow(
			ctx,
			`SELECT `+hypothesisJobColumns+` FROM hypothesis_jobs WHERE id=$1 AND ($2='' OR owner_id=$2)`,
			id,
			owner,
		),
	)
}

func (r *HypothesisJobsRepo) ListHypothesisJobs(
	ctx context.Context,
	owner string,
	limit int,
) ([]*contracts.HypothesisJob, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+hypothesisJobColumns+` FROM hypothesis_jobs WHERE ($1='' OR owner_id=$1) ORDER BY created_at DESC,id DESC LIMIT $2`,
		owner,
		min(
			max(
				limit,
				1,
			),
			1000,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("list hypothesis jobs: %w", err)
	}
	defer rows.Close()
	out := []*contracts.HypothesisJob{}
	for rows.Next() {
		j, e := scanHypothesisJob(rows)
		if e != nil {
			return nil, e
		}
		j.Token = ""
		out = append(out, j)
	}
	if operationErr := rows.Err(); operationErr != nil {
		return out, fmt.Errorf("list hypothesis jobs: %w", operationErr)
	}
	return out, nil
}

// ClaimHypothesisJob восстанавливает брошенную работу и ограничивает число попыток.
func (r *HypothesisJobsRepo) ClaimHypothesisJob(ctx context.Context) (*contracts.HypothesisJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim hypothesis job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE hypothesis_jobs SET state='failed',error='attempts_exhausted',finished_at=now()
 WHERE state='running' AND attempt=3 AND lease_until<=now()`)
	if err != nil {
		return nil, fmt.Errorf("claim hypothesis job: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM hypothesis_jobs WHERE attempt<3 AND not_before<=now()
 AND (state='queued' OR (state='running' AND lease_until<=now())) ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(
		&id,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if operationErr := tx.Commit(ctx); operationErr != nil {
			return nil, fmt.Errorf("claim hypothesis job: %w", operationErr)
		}
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("claim hypothesis job: %w", err)
	}
	out, err := scanHypothesisJob(tx.QueryRow(ctx, `UPDATE hypothesis_jobs SET state='running',attempt=attempt+1,
 token=$2,lease_until=now()+interval '2 minutes',started_at=coalesce(started_at,now()),heartbeat_at=now(),error=''
 WHERE id=$1 RETURNING `+hypothesisJobColumns, id, uuid.NewV4().String()))
	if err != nil {
		return nil, err
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return out, fmt.Errorf("claim hypothesis job: %w", operationErr)
	}
	return out, nil
}

// RenewHypothesisJob не оживляет уже потерянную аренду.
func (r *HypothesisJobsRepo) RenewHypothesisJob(ctx context.Context, id, token string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE hypothesis_jobs SET lease_until=now()+interval '2 minutes',heartbeat_at=now()
 WHERE id=$1 AND token=$2 AND state='running' AND lease_until>now()`, id, token)
	if err != nil {
		return fmt.Errorf("renew hypothesis job: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

// FinishHypothesisJob ограждает терминальную запись тем же токеном, что и побочные эффекты.
func (r *HypothesisJobsRepo) FinishHypothesisJob(ctx context.Context, in *contracts.HypothesisJob) error {
	if in.Status != "succeeded" && in.Status != jobFailedState {
		return domain.ErrInvalidArgument
	}
	tag, err := r.pool.Exec(ctx, `UPDATE hypothesis_jobs SET state=$3,result_ids=$4,error=$5,finished_at=now(),heartbeat_at=now()
 WHERE id=$1 AND token=$2 AND ((state='running' AND lease_until>now()) OR (state=$3 AND result_ids=$4 AND error=$5))`,
		in.ID, in.Token, in.Status, orEmpty(in.ResultIDs), in.Error)
	if err != nil {
		return fmt.Errorf("finish hypothesis job: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

// lockHypothesisEffect блокирует выдачу новой аренды до фиксации прикладной записи.
func lockHypothesisEffect(ctx context.Context, tx pgx.Tx, owner, operation string) (string, error) {
	lease := contracts.HypothesisLeaseFrom(ctx)
	if lease.ID == "" {
		return "", nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT owner_id=$3 AND token=$2 AND state='running' AND lease_until>now()
 FROM hypothesis_jobs WHERE id=$1 FOR UPDATE`, lease.ID, lease.Token, owner).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
		return "", domain.ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("lock hypothesis effect: %w", err)
	}
	var result string
	err = tx.QueryRow(
		ctx,
		`SELECT result_id FROM hypothesis_job_effects WHERE job_id=$1 AND operation=$2`,
		lease.ID,
		operation,
	).Scan(
		&result,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return result, fmt.Errorf("lock hypothesis effect: %w", err)
	}
	return result, nil
}

func saveHypothesisEffect(ctx context.Context, tx pgx.Tx, operation, result string) error {
	lease := contracts.HypothesisLeaseFrom(ctx)
	if lease.ID == "" {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO hypothesis_job_effects(job_id,operation,result_id) VALUES($1,$2,$3)`,
		lease.ID,
		operation,
		result,
	)
	if err != nil {
		return fmt.Errorf("persist job effect: %w", err)
	}
	return nil
}
