package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/jackc/pgx/v5"
)

// ClaimDescription арендует одну попытку; после трёх падений шаблон становится окончательным.
func (r *AnalysisRepo) ClaimDescription(
	ctx context.Context, in analysisdoc.DescriptionClaim,
) (analysisdoc.DescriptionClaimed, error) {
	if err := in.Validate(); err != nil {
		return analysisdoc.DescriptionClaimed{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.DescriptionClaimed{}, fmt.Errorf("begin description claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := scanDescription(tx.QueryRow(ctx, descriptionColumns+`WHERE state='prepared' AND version=$1
AND (lease_until IS NULL OR lease_until<=now())
AND job_id IN (SELECT j.id FROM signal_analysis_jobs j JOIN signal_analysis_inputs i ON i.run_id=j.input_run_id
WHERE i.test_run_id=$2)
ORDER BY created_at,job_id,candidate_id FOR UPDATE SKIP LOCKED LIMIT 1`, in.Version, in.TestRunID))
	if errors.Is(err, analysisdoc.ErrNotFound) {
		return analysisdoc.DescriptionClaimed{}, nil
	}
	if err != nil {
		return analysisdoc.DescriptionClaimed{}, err
	}
	read, err := descriptionOwner(ctx, tx, s.job.JobID, s.job.CandidateID)
	if err != nil {
		return analysisdoc.DescriptionClaimed{}, err
	}
	b, err := descriptionBundle(ctx, tx, read)
	reason := ""
	if errors.Is(err, analysisdoc.ErrNotFound) {
		reason = "source_unavailable"
	} else if err != nil {
		return analysisdoc.DescriptionClaimed{}, err
	}
	if reason == "" && b.SHA256() != s.job.BundleSHA256 {
		return analysisdoc.DescriptionClaimed{}, analysisdoc.ErrConflict
	}
	if s.job.Attempt >= analysisdoc.MaxAttempts {
		reason = "attempts_exhausted"
	}
	if len(s.bundle.Evidence) == 0 {
		reason = "no_facts"
	}
	if reason != "" {
		if err = saveDescription(ctx, tx, s, s.bundle.Template(reason)); err != nil {
			return analysisdoc.DescriptionClaimed{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return analysisdoc.DescriptionClaimed{}, fmt.Errorf("commit description fallback: %w", err)
		}
		return analysisdoc.DescriptionClaimed{}, nil
	}
	token := uuid.NewV4().String()
	_, err = tx.Exec(ctx, `UPDATE signal_descriptions SET attempt=attempt+1,lease_token=$4,
lease_until=now()+$5::interval,updated_at=now() WHERE job_id=$1 AND candidate_id=$2 AND version=$3`,
		s.job.JobID, s.job.CandidateID, s.job.Version, token, analysisdoc.DescriptionLeaseDuration.String())
	if err != nil {
		return analysisdoc.DescriptionClaimed{}, fmt.Errorf("lease description: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.DescriptionClaimed{}, fmt.Errorf("commit description lease: %w", err)
	}
	return analysisdoc.DescriptionClaimed{Lease: &analysisdoc.DescriptionLease{JobID: s.job.JobID,
		CandidateID: s.job.CandidateID, Version: s.job.Version, Token: token}, Bundle: &b}, nil
}

func descriptionOwner(ctx context.Context, tx pgx.Tx, jobID, candidateID string) (analysisdoc.DescriptionRead, error) {
	s, err := readJobState(ctx, tx, jobID, false)
	if err != nil {
		return analysisdoc.DescriptionRead{}, err
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](s.input.JSON)
	return analysisdoc.DescriptionRead{JobID: jobID, ReaderID: s.owner,
		SharedCorpus: input.Request.SharedCorpus, CandidateID: candidateID}, err
}

// CompleteDescription ограждает запись арендой и блокировкой эпохи ACL.
func (r *AnalysisRepo) CompleteDescription(
	ctx context.Context, in analysisdoc.DescriptionComplete,
) (analysisdoc.DescriptionJob, error) {
	if _, err := uuid.Parse(in.Token); err != nil {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("begin description completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := scanDescription(tx.QueryRow(ctx, descriptionColumns+descriptionIdentity+` FOR UPDATE`,
		in.JobID, in.CandidateID, in.Version))
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	if s.token != in.Token {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrConflict
	}
	if err = in.Result.Check(s.bundle); err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	read, err := descriptionOwner(ctx, tx, in.JobID, in.CandidateID)
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT value FROM source_corpus_epoch WHERE id=true FOR SHARE`).Scan(&epoch); err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("fence description access: %w", err)
	}
	b, err := descriptionBundle(ctx, tx, read)
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	if b.SHA256() != s.job.BundleSHA256 {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrConflict
	}
	if s.job.Status != "prepared" {
		want, _ := json.Marshal(s.job.Result)
		got, _ := json.Marshal(in.Result)
		if string(want) != string(got) {
			return analysisdoc.DescriptionJob{}, analysisdoc.ErrConflict
		}
		return s.job, nil
	}
	if s.until == nil || !s.until.After(time.Now()) {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrConflict
	}
	if err = saveDescription(ctx, tx, s, in.Result); err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	s, err = scanDescription(tx.QueryRow(ctx, descriptionColumns+descriptionIdentity, in.JobID, in.CandidateID, in.Version))
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("commit description completion: %w", err)
	}
	return s.job, nil
}

func saveDescription(ctx context.Context, tx pgx.Tx, s descriptionState, result analysisdoc.DescriptionResult) error {
	if err := result.Check(s.bundle); err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode description: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE signal_descriptions SET state=$4,result_json=$5,updated_at=now(),lease_until=NULL
WHERE job_id=$1 AND candidate_id=$2 AND version=$3`, s.job.JobID, s.job.CandidateID, s.job.Version, result.Status, string(raw))
	if err != nil {
		return fmt.Errorf("save description: %w", err)
	}
	return nil
}
