package postgres_test

import (
	"strings"
	"testing"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"uuid"
)

type runFixture struct {
	owner, scopeID, collectionRun, inputRun, jobID string
}

func seedCollectionRun(t *testing.T, db *postgres.DB, owner string, exhausted, incomplete bool) (string, string) {
	t.Helper()
	scopeID, runID := strings.Repeat("a", 63)+"1", uuid.NewV4().String()
	if _, err := db.Pool.Exec(t.Context(), `INSERT INTO source_collection_scopes
(id,owner_id,provider,scope,run_id,lease_token,run_limit,status)
VALUES($1,$2,'crossref','{"query":"hydrogen"}'::jsonb,$3,'',200,'partial')
ON CONFLICT (id) DO NOTHING`, scopeID, owner, runID); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	completeness := "partial"
	if exhausted && !incomplete {
		completeness = "complete"
	}
	if _, err := db.Pool.Exec(t.Context(), `INSERT INTO collection_manifests
(scope_id,run_id,owner_id,provider,scope,run_limit,cursor,pages,listed,received,rejected,processed,
exhausted,incomplete,completeness)
VALUES($1,$2,$3,'crossref','{"query":"hydrogen"}'::jsonb,200,'',1,20,18,2,18,$4,$5,$6)`,
		scopeID, runID, owner, exhausted, incomplete, completeness); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	return scopeID, runID
}

func seedAnalysisJob(t *testing.T, db *postgres.DB, owner string) (string, string) {
	t.Helper()
	inputRun, jobID := uuid.NewV4().String(), uuid.NewV4().String()
	hash := strings.Repeat("b", 64)
	if _, err := db.Pool.Exec(t.Context(), `INSERT INTO signal_analysis_inputs
(run_id,owner_id,request_id,request_hash,manifest,manifest_hash)
VALUES($1,$2,$3,$4,'{}',$4)`, inputRun, owner, uuid.NewV4().String(), hash); err != nil {
		t.Fatalf("seed input: %v", err)
	}
	if _, err := db.Pool.Exec(t.Context(), `INSERT INTO signal_analysis_jobs
(id,input_run_id,method_json,method_sha256,scope_key) VALUES($1,$2,'{}',$3,'scope')`,
		jobID, inputRun, hash); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	return inputRun, jobID
}

func insertRun(t *testing.T, db *postgres.DB, f runFixture, metrics, parser, previousRun string, manifests bool) error {
	t.Helper()
	tx, err := db.Pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	var previous *string
	if previousRun != "" {
		previous = &previousRun
	}
	if _, err = tx.Exec(t.Context(), `INSERT INTO analysis_runs
(id,owner_id,request_id,input_run_id,cutoff,history_mode,extraction_version,embedding_version,
identity_version,clustering_version,metrics_version,parser_version,method_version,method_sha256,
result_sha256,comparable_with)
VALUES($1,$2,$3,$4,'2026-09-11T00:00:00Z','as_of','brief-v12','f2llm@r1/1024','identity-v1',
'candidates-v2',$5,$6,'method-v6',$7,$8,$9)`,
		f.jobID, f.owner, uuid.NewV4().String(), f.inputRun, metrics, parser,
		strings.Repeat("b", 64), strings.Repeat("c", 64), previous); err != nil {
		return err
	}
	if manifests {
		if _, err = tx.Exec(t.Context(), `INSERT INTO analysis_run_manifests(run_id,scope_id,collection_run_id)
VALUES($1,$2,$3)`, f.jobID, f.scopeID, f.collectionRun); err != nil {
			return err
		}
	}
	return tx.Commit(t.Context())
}

func TestManifestsAndRuns(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	scopeID, collectionRun := seedCollectionRun(t, db, owner, true, false)
	firstInput, firstJob := seedAnalysisJob(t, db, owner)
	first := runFixture{owner: owner, scopeID: scopeID, collectionRun: collectionRun,
		inputRun: firstInput, jobID: firstJob}

	if err := insertRun(t, db, first, "quarters-v3/emergence-v3", "pdf-v4", "", true); err != nil {
		t.Fatalf("publish first run: %v", err)
	}

	secondInput, secondJob := seedAnalysisJob(t, db, owner)
	second := runFixture{owner: owner, scopeID: scopeID, collectionRun: collectionRun,
		inputRun: secondInput, jobID: secondJob}
	if err := insertRun(t, db, second, "quarters-v4/emergence-v4", "pdf-v4", "", true); err != nil {
		t.Fatalf("publish second run: %v", err)
	}

	var runs, sharedManifests int
	if err := db.Pool.QueryRow(ctx, `SELECT count(DISTINCT r.id), count(DISTINCT m.collection_run_id)
FROM analysis_runs r JOIN analysis_run_manifests m ON m.run_id=r.id WHERE r.owner_id=$1`,
		owner).Scan(&runs, &sharedManifests); err != nil {
		t.Fatalf("read runs: %v", err)
	}
	if runs != 2 || sharedManifests != 1 {
		t.Fatalf("expected two runs over one manifest, got %d runs and %d manifests", runs, sharedManifests)
	}

	var metrics string
	if err := db.Pool.QueryRow(ctx, `SELECT metrics_version FROM analysis_runs WHERE id=$1`,
		firstJob).Scan(&metrics); err != nil || metrics != "quarters-v3/emergence-v3" {
		t.Fatalf("old run lost its own metrics version: %q %v", metrics, err)
	}

	t.Run("comparable across a changed version is refused", func(t *testing.T) {
		_, thirdJob := seedAnalysisJob(t, db, owner)
		third := runFixture{owner: owner, scopeID: scopeID, collectionRun: collectionRun,
			inputRun: first.inputRun, jobID: thirdJob}
		if insertRun(t, db, third, "quarters-v3/emergence-v3", "pdf-v5", firstJob, true) == nil {
			t.Fatal("a reparsed run was declared comparable with the previous one")
		}
		if err := insertRun(t, db, third, "quarters-v3/emergence-v3", "pdf-v4", firstJob, true); err != nil {
			t.Fatalf("identical versions must stay comparable: %v", err)
		}
	})

	t.Run("run without input manifests is refused", func(t *testing.T) {
		_, jobID := seedAnalysisJob(t, db, owner)
		bare := runFixture{owner: owner, scopeID: scopeID, collectionRun: collectionRun,
			inputRun: first.inputRun, jobID: jobID}
		if insertRun(t, db, bare, "quarters-v3/emergence-v3", "pdf-v4", "", false) == nil {
			t.Fatal("a run with no recorded input was published")
		}
	})

	t.Run("republishing the same run is idempotent", func(t *testing.T) {
		_, err := db.Pool.Exec(ctx, `INSERT INTO analysis_runs
(id,owner_id,request_id,input_run_id,cutoff,history_mode,extraction_version,embedding_version,
identity_version,clustering_version,metrics_version,parser_version,method_version,method_sha256,result_sha256)
VALUES($1,$2,$3,$4,'2026-09-11T00:00:00Z','as_of','other','other','other','other','other','other',
'other',$5,$6) ON CONFLICT (id) DO NOTHING`,
			firstJob, owner, uuid.NewV4().String(), first.inputRun, strings.Repeat("b", 64), strings.Repeat("c", 64))
		if err != nil {
			t.Fatalf("republish: %v", err)
		}
		var kept string
		if err = db.Pool.QueryRow(ctx, `SELECT extraction_version FROM analysis_runs WHERE id=$1`,
			firstJob).Scan(&kept); err != nil || kept != "brief-v12" {
			t.Fatalf("republish overwrote the recorded run: %q %v", kept, err)
		}
	})

	t.Run("an unexhausted collection is never complete", func(t *testing.T) {
		_, err := db.Pool.Exec(ctx, `INSERT INTO collection_manifests
(scope_id,run_id,owner_id,provider,scope,run_limit,pages,listed,received,rejected,processed,
exhausted,incomplete,completeness)
VALUES($1,$2,$3,'crossref','{}'::jsonb,200,1,20,18,2,18,false,false,'complete')`,
			scopeID, uuid.NewV4().String(), owner)
		if err == nil {
			t.Fatal("an unexhausted scope claimed complete coverage")
		}
		_, err = db.Pool.Exec(ctx, `INSERT INTO collection_manifests
(scope_id,run_id,owner_id,provider,scope,run_limit,pages,listed,received,rejected,processed,
exhausted,incomplete,completeness)
VALUES($1,$2,$3,'crossref','{}'::jsonb,200,1,20,18,2,18,true,true,'complete')`,
			scopeID, uuid.NewV4().String(), owner)
		if err == nil {
			t.Fatal("a scope with rejected records claimed complete coverage")
		}
	})
}
