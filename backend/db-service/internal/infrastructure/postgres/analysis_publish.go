package postgres

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/jackc/pgx/v5"
)

func (r *AnalysisRepo) publicationState(ctx context.Context, lease analysisdoc.Lease) (jobState, error) {
	if err := lease.Validate(); err != nil {
		return jobState{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return jobState{}, fmt.Errorf("begin analysis validation read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := readJobState(ctx, tx, lease.JobID, false)
	if err != nil {
		return s, err
	}
	if s.job.Status == analysisdoc.Published && s.token == lease.Token {
		return s, nil
	}
	return s, checkJobLease(s, lease, analysisdoc.Prepared, analysisdoc.Validated)
}

func (r *AnalysisRepo) verifyPublication(ctx context.Context, s jobState, raw string) error {
	input, err := analysisdoc.Decode[analysisdoc.Input](s.input.JSON)
	if err != nil || input.Version != analysisdoc.Version {
		return analysisdoc.ErrInvalid
	}
	if _, err = analysisdoc.ParseMethod(s.job.MethodJSON); err != nil {
		return err
	}
	if err = analysisdoc.ValidateResult(raw, s.job, input); err != nil {
		return err
	}
	if err = analysisdoc.VerifyResultSources(ctx, r.objects, input, raw); err != nil {
		return err
	}
	for key, body := range map[string]string{
		s.input.Artifact: s.input.JSON,
		analysisdoc.MethodKey(s.job.MethodSHA256):                   s.job.MethodJSON,
		analysisdoc.ResultKey(s.job.ID, indexdoc.Hash([]byte(raw))): raw,
	} {
		if err = analysisdoc.VerifyObject(ctx, r.objects, key, body); err != nil {
			return err
		}
	}
	return nil
}

// Validate готовит проверенную проекцию; восстановленная аренда продолжает с уже записанных байтов.
func (r *AnalysisRepo) Validate(ctx context.Context, in analysisdoc.Validate) (analysisdoc.Job, error) {
	s, err := r.publicationState(ctx, in.Lease)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if s.job.ResultJSON != "" && s.job.ResultJSON != in.ResultJSON {
		return analysisdoc.Job{}, analysisdoc.ErrConflict
	}
	if err = r.verifyPublication(ctx, s, in.ResultJSON); err != nil {
		return analysisdoc.Job{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("begin analysis validation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err = readJobState(ctx, tx, in.JobID, true)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if s.job.ResultJSON != "" && s.job.ResultJSON != in.ResultJSON {
		return analysisdoc.Job{}, analysisdoc.ErrConflict
	}
	if s.job.Status == analysisdoc.Published && s.token == in.Token {
		return s.job, nil
	}
	if err = checkJobLease(s, in.Lease, analysisdoc.Prepared, analysisdoc.Validated); err != nil {
		return analysisdoc.Job{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET state='validated',result_json=$2,result_sha256=$3,
updated_at=now() WHERE id=$1`, in.JobID, in.ResultJSON, indexdoc.Hash([]byte(in.ResultJSON)))
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("save validated analysis: %w", err)
	}
	s, err = readJobState(ctx, tx, in.JobID, false)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("commit analysis validation: %w", err)
	}
	return s.job, nil
}

// Publish перепроверяет объекты и аренду, затем активирует всё одной транзакцией.
func (r *AnalysisRepo) Publish(ctx context.Context, in analysisdoc.Lease) (analysisdoc.Job, error) {
	s, err := r.publicationState(ctx, in)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if s.job.Status != analysisdoc.Validated && s.job.Status != analysisdoc.Published {
		return analysisdoc.Job{}, analysisdoc.ErrConflict
	}
	if err = r.verifyPublication(ctx, s, s.job.ResultJSON); err != nil {
		return analysisdoc.Job{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("begin analysis publication: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err = readJobState(ctx, tx, in.JobID, true)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if s.job.Status == analysisdoc.Published && s.token == in.Token {
		return s.job, nil
	}
	if err = checkJobLease(s, in, analysisdoc.Validated); err != nil {
		return analysisdoc.Job{}, err
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](s.input.JSON)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	method, err := analysisdoc.ParseMethod(s.job.MethodJSON)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	canReplaceEvaluated, err := evaluatedReplacement(s.job, method)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	// Отзыв тоже обновляет эту строку, поэтому не может вклиниться в активацию.
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT value FROM source_corpus_epoch WHERE id=true FOR SHARE`).Scan(&epoch); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("fence analysis access: %w", err)
	}
	if err = checkAnalysisAccess(ctx, tx, s.job.InputRunID, s.owner, input.Request.SharedCorpus); err != nil {
		return analysisdoc.Job{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE signal_analysis_jobs SET state='published',updated_at=now() WHERE id=$1`, in.JobID)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("publish analysis: %w", err)
	}
	if err = saveAnalysisRun(ctx, tx, s, input, method); err != nil {
		return analysisdoc.Job{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO signal_analysis_heads(owner_id,scope_key,job_id,input_created_at,generation)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner_id,scope_key) DO UPDATE SET job_id=excluded.job_id,
input_created_at=excluded.input_created_at,generation=excluded.generation
WHERE (signal_analysis_heads.input_created_at,signal_analysis_heads.generation)
    < (excluded.input_created_at,excluded.generation)
AND ($6 OR NOT EXISTS (SELECT 1 FROM signal_analysis_jobs previous
WHERE previous.id=signal_analysis_heads.job_id AND previous.method_json::jsonb->>'quality_status'='evaluated'))`,
		s.owner, s.scope, s.job.ID, s.inputCreated, s.generation, canReplaceEvaluated)
	if err != nil {
		return analysisdoc.Job{}, fmt.Errorf("activate analysis head: %w", err)
	}
	s, err = readJobState(ctx, tx, in.JobID, false)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return analysisdoc.Job{}, fmt.Errorf("commit full analysis: %w", err)
	}
	return s.job, nil
}

// saveAnalysisRun пишет происхождение прогона: этого требует отложенное ограничение.
func saveAnalysisRun(ctx context.Context, tx pgx.Tx, s jobState,
	input analysisdoc.Input, method analysisdoc.Method,
) error {
	versions, err := analysisdoc.Versions(method, input)
	if err != nil {
		return err
	}
	limitations, err := json.Marshal(input.Limitations)
	if err != nil {
		return fmt.Errorf("encode run limitations: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO analysis_runs(id,owner_id,request_id,input_run_id,cutoff,history_mode,
extraction_version,embedding_version,identity_version,clustering_version,metrics_version,parser_version,
method_version,method_sha256,result_sha256,limitations)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT (id) DO NOTHING`,
		// Замороженный вход хранит слова, с которыми опубликован (их фиксирует хеш),
		// поэтому режим переводится на текущий словарь здесь.
		s.job.ID, s.owner, input.Request.RequestID, s.job.InputRunID, input.Request.Cutoff,
		analysisdoc.NormalizeMode(input.Request.Mode),
		versions.Extraction, versions.Embedding, versions.Identity, versions.Clustering, versions.Metrics,
		versions.Parser, method.Version, s.job.MethodSHA256, s.job.ResultSHA256, limitations)
	if err != nil {
		return fmt.Errorf("record analysis run: %w", err)
	}
	for _, page := range input.Pages {
		_, err = tx.Exec(ctx, `INSERT INTO analysis_run_manifests(run_id,scope_id,collection_run_id)
VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, s.job.ID, page.ScopeID, page.RunID)
		if err != nil {
			return fmt.Errorf("record analysis input manifest: %w", err)
		}
	}
	return nil
}

func evaluatedReplacement(job analysisdoc.Job, method analysisdoc.Method) (bool, error) {
	result, err := analysisdoc.Decode[analysisdoc.Result](job.ResultJSON)
	if err != nil {
		return false, err
	}
	return method.QualityStatus == "evaluated" && result.Ranking.Completeness == analysisdoc.RankingComplete, nil
}
