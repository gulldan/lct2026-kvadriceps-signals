package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/jackc/pgx/v5"
)

// Claim берёт аренду в PostgreSQL; упавший воркер восстанавливается после её истечения.
func (r *AnalysisRepo) Claim(ctx context.Context, in analysisdoc.Claim) (analysisdoc.Claimed, error) {
	if _, err := uuid.Parse(in.WorkerID); err != nil || !analysisdoc.ValidHash(in.MethodSHA256) {
		return analysisdoc.Claimed{}, analysisdoc.ErrInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.Claimed{}, fmt.Errorf("begin analysis claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET state='failed',failure_code='attempts_exhausted',updated_at=now()
WHERE method_sha256=$1 AND state IN ('prepared','validated') AND attempt=3 AND lease_until<now()`, in.MethodSHA256)
	if err != nil {
		return analysisdoc.Claimed{}, fmt.Errorf("expire analysis attempts: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM signal_analysis_jobs WHERE method_sha256=$1
AND state IN ('prepared','validated') AND attempt<3 AND not_before<=now()
AND (lease_until IS NULL OR lease_until<now()) ORDER BY generation LIMIT 1 FOR UPDATE SKIP LOCKED`,
		in.MethodSHA256).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return analysisdoc.Claimed{}, fmt.Errorf("commit empty analysis queue: %w", err)
		}
		return analysisdoc.Claimed{}, nil
	}
	if err != nil {
		return analysisdoc.Claimed{}, fmt.Errorf("claim analysis job: %w", err)
	}
	token := uuid.NewV4().String()
	_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET attempt=attempt+1,lease_token=$2,
lease_until=now()+interval '2 minutes',updated_at=now(),failure_code='' WHERE id=$1`, id, token)
	if err != nil {
		return analysisdoc.Claimed{}, fmt.Errorf("lease analysis job: %w", err)
	}
	s, err := readJobState(ctx, tx, id, false)
	if err != nil {
		return analysisdoc.Claimed{}, err
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](s.input.JSON)
	if err != nil {
		return analysisdoc.Claimed{}, err
	}
	if err = checkAnalysisAccess(ctx, tx, s.job.InputRunID, s.owner, input.Request.SharedCorpus); err != nil {
		if !errors.Is(err, analysisdoc.ErrNotFound) {
			return analysisdoc.Claimed{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET state='failed',failure_code='access_changed',
lease_until=NULL,updated_at=now() WHERE id=$1`, id)
		if err != nil {
			return analysisdoc.Claimed{}, fmt.Errorf("invalidate inaccessible analysis: %w", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return analysisdoc.Claimed{}, fmt.Errorf("commit analysis invalidation: %w", err)
		}
		return analysisdoc.Claimed{}, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Claimed{}, fmt.Errorf("commit analysis claim: %w", err)
	}
	return analysisdoc.Claimed{Job: &s.job, Lease: &analysisdoc.Lease{JobID: id, Token: token}, Input: s.input}, nil
}

// Renew продлевает только текущую неистёкшую попытку.
func (r *AnalysisRepo) Renew(ctx context.Context, in analysisdoc.Lease) (analysisdoc.Job, error) {
	if err := in.Validate(); err != nil {
		return analysisdoc.Job{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("begin analysis renewal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := readJobState(ctx, tx, in.JobID, true)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if err = checkJobLease(s, in, analysisdoc.Prepared, analysisdoc.Validated); err != nil {
		return analysisdoc.Job{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=$1`,
		in.JobID)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("renew analysis lease: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("commit analysis renewal: %w", err)
	}
	return s.job, nil
}

func (r *AnalysisRepo) Fail(ctx context.Context, in analysisdoc.Failure) (analysisdoc.Job, error) {
	if err := in.Validate(); err != nil {
		return analysisdoc.Job{}, err
	}
	if !slices.Contains([]string{"artifact_unavailable", "invalid_input", "compute_unavailable",
		"invalid_result", "access_changed", "worker_failure"}, in.Code) {
		return analysisdoc.Job{}, analysisdoc.ErrInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("begin analysis failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := readJobState(ctx, tx, in.JobID, true)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if err = checkJobLease(s, in.Lease, analysisdoc.Prepared, analysisdoc.Validated); err != nil {
		return analysisdoc.Job{}, err
	}
	state := analysisdoc.Failed
	if in.Retryable && s.job.Attempt < analysisdoc.MaxAttempts {
		state = s.job.Status
	}
	_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET state=$2,failure_code=$3,lease_token='',lease_until=NULL,
not_before=now()+interval '30 seconds',updated_at=now() WHERE id=$1`, in.JobID, state, in.Code)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("save analysis failure: %w", err)
	}
	s, err = readJobState(ctx, tx, in.JobID, false)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("commit analysis failure: %w", err)
	}
	s.job.ResultJSON = ""
	return s.job, nil
}
