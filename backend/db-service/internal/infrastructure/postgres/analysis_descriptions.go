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

type descriptionState struct {
	job    analysisdoc.DescriptionJob
	bundle analysisdoc.FactBundle
	token  string
	until  *time.Time
}

const descriptionColumns = `SELECT job_id,candidate_id,version,bundle_sha256,state,attempt,
result_json,updated_at,bundle_json,lease_token,lease_until FROM signal_descriptions `

const descriptionIdentity = `WHERE job_id=$1 AND candidate_id=$2 AND version=$3`

func scanDescription(row pgx.Row) (descriptionState, error) {
	var s descriptionState
	var result, bundle string
	err := row.Scan(&s.job.JobID, &s.job.CandidateID, &s.job.Version, &s.job.BundleSHA256, &s.job.Status,
		&s.job.Attempt, &result, &s.job.UpdatedAt, &bundle, &s.token, &s.until)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, analysisdoc.ErrNotFound
	}
	if err != nil {
		return s, fmt.Errorf("read description: %w", err)
	}
	s.bundle, err = analysisdoc.Decode[analysisdoc.FactBundle](bundle)
	if err != nil || s.bundle.Validate() != nil || s.bundle.SHA256() != s.job.BundleSHA256 ||
		s.bundle.JobID != s.job.JobID || s.bundle.CandidateID != s.job.CandidateID || s.bundle.Version != s.job.Version {
		return s, analysisdoc.ErrInvalid
	}
	s.job.ResultSHA256 = s.bundle.ResultSHA256
	s.job.EvidenceSHA256 = s.bundle.EvidenceSHA256
	if result != "" {
		var d analysisdoc.DescriptionResult
		if json.Unmarshal([]byte(result), &d, json.RejectUnknownMembers(true)) != nil ||
			d.Check(s.bundle) != nil || d.Status != s.job.Status {
			return s, analysisdoc.ErrInvalid
		}
		s.job.Result = &d
	}
	return s, nil
}

func descriptionBundle(ctx context.Context, tx pgx.Tx, in analysisdoc.DescriptionRead) (analysisdoc.FactBundle, error) {
	if _, err := uuid.Parse(in.JobID); err != nil {
		return analysisdoc.FactBundle{}, analysisdoc.ErrInvalid
	}
	if _, err := uuid.Parse(in.ReaderID); err != nil {
		return analysisdoc.FactBundle{}, analysisdoc.ErrInvalid
	}
	s, err := readJobState(ctx, tx, in.JobID, false)
	if err != nil {
		return analysisdoc.FactBundle{}, err
	}
	if s.owner != in.ReaderID {
		return analysisdoc.FactBundle{}, analysisdoc.ErrNotFound
	}
	if err = checkAnalysisAccess(ctx, tx, s.job.InputRunID, in.ReaderID, in.SharedCorpus); err != nil {
		return analysisdoc.FactBundle{}, err
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](s.input.JSON)
	if err != nil {
		return analysisdoc.FactBundle{}, err
	}
	for _, d := range input.Records {
		s.job.Sources = append(s.job.Sources, analysisdoc.SourceRef{DocumentID: d.ID, SnapshotID: d.Index.SnapshotID,
			Title: d.Title, Source: d.Source, TextSHA256: d.Index.Text.SHA256})
	}
	return analysisdoc.BuildFactBundle(s.job, in.CandidateID)
}

// StartDescription идемпотентен и ограничен; опубликованные факты не трогает.
func (r *AnalysisRepo) StartDescription(ctx context.Context, in analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("begin description: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Одна короткая блокировка допуска убирает гонку за общий потолок и потолок владельца.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7401823)`); err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("lock description admission: %w", err)
	}
	b, err := descriptionBundle(ctx, tx, in)
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	s, err := scanDescription(tx.QueryRow(ctx, descriptionColumns+descriptionIdentity, in.JobID, in.CandidateID, b.Version))
	if err == nil {
		if s.job.BundleSHA256 != b.SHA256() {
			return analysisdoc.DescriptionJob{}, analysisdoc.ErrConflict
		}
		return s.job, nil
	}
	if !errors.Is(err, analysisdoc.ErrNotFound) {
		return analysisdoc.DescriptionJob{}, err
	}
	var total, owned int
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE i.owner_id=$1 AND
i.test_run_id=(SELECT si.test_run_id
FROM signal_analysis_jobs sj JOIN signal_analysis_inputs si ON si.run_id=sj.input_run_id WHERE sj.id=$2))
FROM signal_descriptions d
JOIN signal_analysis_jobs j ON j.id=d.job_id JOIN signal_analysis_inputs i ON i.run_id=j.input_run_id
WHERE d.state='prepared'`, in.ReaderID, in.JobID).Scan(&total, &owned)
	if err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("count pending descriptions: %w", err)
	}
	if total >= 100 || owned >= 16 {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrCapacity
	}
	raw, _ := b.Bytes()
	_, err = tx.Exec(ctx, `INSERT INTO signal_descriptions(job_id,candidate_id,version,bundle_json,bundle_sha256)
VALUES($1,$2,$3,$4,$5)`, in.JobID, in.CandidateID, b.Version, string(raw), b.SHA256())
	if err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("queue description: %w", err)
	}
	s, err = scanDescription(tx.QueryRow(ctx, descriptionColumns+descriptionIdentity, in.JobID, in.CandidateID, b.Version))
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("commit queued description: %w", err)
	}
	return s.job, nil
}

// ReadDescription заново проверяет весь ACL входа и идентичность доказательств.
func (r *AnalysisRepo) ReadDescription(ctx context.Context, in analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return analysisdoc.DescriptionJob{}, fmt.Errorf("begin description read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	b, err := descriptionBundle(ctx, tx, in)
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	s, err := scanDescription(tx.QueryRow(ctx, descriptionColumns+descriptionIdentity, in.JobID, in.CandidateID, b.Version))
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	if s.job.BundleSHA256 != b.SHA256() {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrConflict
	}
	return s.job, nil
}
