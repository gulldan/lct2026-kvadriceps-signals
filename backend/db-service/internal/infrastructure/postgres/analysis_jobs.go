package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/jackc/pgx/v5"
)

type jobState struct {
	job                 analysisdoc.Job
	input               analysisdoc.Frozen
	owner, scope, token string
	generation          int64
	inputCreated        time.Time
	leaseUntil          *time.Time
}

const jobColumns = `SELECT j.id,j.input_run_id,i.manifest_hash,j.method_sha256,j.method_json,
j.state,j.attempt,j.failure_code,j.created_at,j.updated_at,j.result_sha256,j.result_json,
EXISTS(SELECT 1 FROM signal_analysis_heads h WHERE h.job_id=j.id),
i.manifest,i.owner_id,j.scope_key,j.lease_token,j.generation,i.created_at,j.lease_until
FROM signal_analysis_jobs j JOIN signal_analysis_inputs i ON i.run_id=j.input_run_id `

func readJobState(ctx context.Context, tx pgx.Tx, id string, lock bool) (jobState, error) {
	var s jobState
	query := jobColumns + `WHERE j.id=$1`
	if lock {
		query += ` FOR UPDATE OF j`
	}
	err := tx.QueryRow(ctx, query, id).Scan(&s.job.ID, &s.job.InputRunID, &s.job.InputSHA256,
		&s.job.MethodSHA256, &s.job.MethodJSON, &s.job.Status, &s.job.Attempt, &s.job.FailureCode,
		&s.job.CreatedAt, &s.job.UpdatedAt, &s.job.ResultSHA256, &s.job.ResultJSON, &s.job.Active,
		&s.input.JSON, &s.owner, &s.scope, &s.token, &s.generation, &s.inputCreated, &s.leaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, analysisdoc.ErrNotFound
	}
	if err != nil {
		return s, fmt.Errorf("read analysis job: %w", err)
	}
	s.input.RunID, s.input.SHA256 = s.job.InputRunID, s.job.InputSHA256
	s.input, err = finishFrozen(s.input)
	if err != nil {
		return s, err
	}
	if indexdoc.Hash([]byte(s.job.MethodJSON)) != s.job.MethodSHA256 ||
		(s.job.ResultJSON != "" && indexdoc.Hash([]byte(s.job.ResultJSON)) != s.job.ResultSHA256) {
		return s, analysisdoc.ErrInvalid
	}
	if s.job.ResultSHA256 != "" {
		s.job.ResultArtifact = analysisdoc.ResultKey(s.job.ID, s.job.ResultSHA256)
	}
	return s, nil
}

func (r *AnalysisRepo) Submit(ctx context.Context, in analysisdoc.Submit) (analysisdoc.Job, error) {
	if _, err := analysisdoc.ParseMethod(in.MethodJSON); err != nil {
		return analysisdoc.Job{}, err
	}
	frozen, err := r.Read(ctx, in.Read)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](frozen.JSON)
	if err != nil || input.Version != analysisdoc.Version {
		return analysisdoc.Job{}, analysisdoc.ErrInvalid
	}
	if err = analysisdoc.VerifyObject(ctx, r.objects, frozen.Artifact, frozen.JSON); err != nil {
		return analysisdoc.Job{}, err
	}
	// RequestID отвечает за идемпотентность, а не за охват. Тестовое пространство не может
	// активировать рабочую голову, даже при совпадающих запросе и владельце.
	scope := input.Request
	scope.RequestID = ""
	raw, err := json.Marshal(scope, json.Deterministic(true))
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("encode analysis scope: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("begin analysis submit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// ponytail: одна короткая блокировка допуска; делить, только если замер покажет конкуренцию
	// за submit. Модель и чтение источников и S3 идут вне неё.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7401824)`); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("lock analysis admission: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM signal_analysis_jobs WHERE input_run_id=$1 AND method_sha256=$2`,
		in.RunID, indexdoc.Hash([]byte(in.MethodJSON))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = analysisJobCapacity(ctx, tx, input.Request); err != nil {
			return analysisdoc.Job{}, err
		}
		err = tx.QueryRow(ctx, `INSERT INTO signal_analysis_jobs(id,input_run_id,method_json,method_sha256,scope_key)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(input_run_id,method_sha256)
DO UPDATE SET input_run_id=excluded.input_run_id RETURNING id`, uuid.NewV4().String(), in.RunID, in.MethodJSON,
			indexdoc.Hash([]byte(in.MethodJSON)), indexdoc.Hash(raw)).Scan(&id)
	}
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("submit analysis: %w", err)
	}
	s, err := readJobState(ctx, tx, id, false)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if s.job.MethodJSON != in.MethodJSON {
		return analysisdoc.Job{}, analysisdoc.ErrConflict
	}
	if err = checkAnalysisAccess(ctx, tx, in.RunID, in.ReaderID, in.SharedCorpus); err != nil {
		return analysisdoc.Job{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("commit analysis submit: %w", err)
	}
	if s.job.Status != analysisdoc.Published {
		s.job.ResultJSON = ""
	}
	return s.job, nil
}

// ReadJob скрывает неопубликованные данные и перепроверяет доступ читателя.
func (r *AnalysisRepo) ReadJob(ctx context.Context, in analysisdoc.JobRead) (analysisdoc.Job, error) {
	if _, err := uuid.Parse(in.JobID); err != nil {
		return analysisdoc.Job{}, analysisdoc.ErrInvalid
	}
	if _, err := uuid.Parse(in.ReaderID); err != nil {
		return analysisdoc.Job{}, analysisdoc.ErrInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("begin analysis result read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := readJobState(ctx, tx, in.JobID, false)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if s.owner != in.ReaderID {
		return analysisdoc.Job{}, analysisdoc.ErrNotFound
	}
	if err = checkAnalysisAccess(ctx, tx, s.job.InputRunID, in.ReaderID, in.SharedCorpus); err != nil {
		return analysisdoc.Job{}, err
	}
	input, decodeErr := analysisdoc.Decode[analysisdoc.Input](s.input.JSON)
	if decodeErr != nil {
		return analysisdoc.Job{}, decodeErr
	}
	s.job.Request = &input.Request
	if s.job.Status != analysisdoc.Published {
		s.job.ResultJSON = ""
	} else {
		for _, record := range input.Records {
			s.job.Sources = append(s.job.Sources, analysisdoc.SourceRef{DocumentID: record.ID,
				SnapshotID: record.Index.SnapshotID, Title: record.Title, Source: record.Source, TextSHA256: record.Index.Text.SHA256})
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("commit analysis result read: %w", err)
	}
	return s.job, nil
}

func checkJobLease(s jobState, in analysisdoc.Lease, states ...string) error {
	if s.token != in.Token || s.leaseUntil == nil || !s.leaseUntil.After(time.Now()) {
		return analysisdoc.ErrConflict
	}
	for _, state := range states {
		if s.job.Status == state {
			return nil
		}
	}
	return analysisdoc.ErrConflict
}
