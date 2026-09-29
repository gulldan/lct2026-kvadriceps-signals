package postgres_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
)

type publicationObjects map[string]string

func (objects publicationObjects) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	raw, err := io.ReadAll(r)
	if err == nil {
		objects[key] = string(raw)
	}
	return err
}

func (objects publicationObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	value, ok := objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func TestTechnologyResultPublishesAndReads(t *testing.T) {
	db := newTestDB(t)
	owner := uuid.NewV4().String()
	revision := seedPublicDoc(t, db, owner)
	scopeID, collectionRun := seedCollectionRun(t, db, owner, true, false)
	inputRun, jobID := seedAnalysisJob(t, db, owner)
	lease := analysisdoc.Lease{JobID: jobID, Token: uuid.NewV4().String()}
	quote := "Мембранный электролиз"
	objects := publicationObjects{"audit/source": quote}
	input := analysisdoc.Input{Version: analysisdoc.Version, RunID: inputRun,
		Request: analysisdoc.Request{OwnerID: owner, RequestID: uuid.NewV4().String(), Query: "электролиз",
			Cutoff: "2026-01-01T00:00:00Z", Mode: analysisdoc.ModeAsOf, AnalysisKind: analysisdoc.KindBriefV1},
		Records: []analysisdoc.Record{{ID: revision, Index: indexdoc.Manifest{ParserRevision: "audit-parser-v1",
			Text: indexdoc.Object{Key: "audit/source", SHA256: indexdoc.Hash([]byte(quote))}}}},
		Pages: []analysisdoc.Page{{ScopeID: scopeID, RunID: collectionRun}}}
	encode := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	inputJSON := encode(input)
	inputHash := indexdoc.Hash([]byte(inputJSON))
	if _, err := db.Pool.Exec(t.Context(), `INSERT INTO signal_analysis_sources(run_id,document_id,snapshot_id) VALUES($1,$2,'')`,
		inputRun, revision); err != nil {
		t.Fatal(err)
	}
	methodJSON := encode(analysisdoc.Method{Version: "audit-publication-v1", CodeSHA256: indexdoc.Hash([]byte("audit")),
		QualityStatus: "not_frozen", Explanation: jsontext.Value(`{"test_only":true}`),
		Parameters: jsontext.Value(`{"brief_version":"audit-extract-v1","candidates_version":"audit-cluster-v1",
"emergence_version":"audit-stage-v1","versions":{"identity":"audit-id-v1","measurements":"audit-metrics-v1"}}`)})
	methodHash := indexdoc.Hash([]byte(methodJSON))
	if _, err := db.Pool.Exec(t.Context(), `UPDATE signal_analysis_inputs SET manifest=$2,manifest_hash=$3 WHERE run_id=$1`,
		inputRun, inputJSON, inputHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(t.Context(), `UPDATE signal_analysis_jobs SET method_json=$2,method_sha256=$3,
state='prepared',lease_token=$4,lease_until=now()+interval '5 minutes' WHERE id=$1`,
		jobID, methodJSON, methodHash, lease.Token); err != nil {
		t.Fatal(err)
	}
	resultJSON := encode(analysisdoc.Result{Version: analysisdoc.ResultVersion,
		JobID: jobID, InputRunID: inputRun, InputSHA256: inputHash, MethodSHA256: methodHash,
		Cutoff: input.Request.Cutoff, Query: input.Request.Query,
		Coverage:   jsontext.Value(`{"test_only":true}`),
		Works:      []analysisdoc.Work{{ID: "audit-work", DocumentIDs: []string{revision}}},
		Unassigned: []string{"audit-work"},
		Ranking:    analysisdoc.Ranking{Completeness: "insufficient_coverage", Reasons: []string{"test_only"}},
		Technologies: []analysisdoc.ResultTechnology{{TechnologyID: "audit-technology", Name: quote,
			Role: "proposed_contribution", Support: analysisdoc.TechnologySupport{Numerator: 1, Denominator: 1},
			Evidence: []analysisdoc.ResultQuote{{DocumentID: revision, Start: 0, End: len([]rune(quote)), Quote: quote}}}},
		Directions: []analysisdoc.ResultDirection{{DirectionID: "audit-direction", Title: "Водород",
			Technologies: []string{"audit-technology"}, Stage: "early_signal",
			Measurements: jsontext.Value(`{"velocity":{"value":null,"reason":"insufficient_history"}}`)}},
		TechnologyOrder: analysisdoc.ResultTechnologyOrder{Rule: analysisdoc.OrderBySupport, OrderedIDs: []string{"audit-technology"}}})
	objects[analysisdoc.ArtifactKey(inputRun, inputHash)] = inputJSON
	objects[analysisdoc.MethodKey(methodHash)] = methodJSON
	objects[analysisdoc.ResultKey(jobID, indexdoc.Hash([]byte(resultJSON)))] = resultJSON
	repo := postgres.NewAnalysisJobs(db, objects)
	if _, err := repo.Validate(t.Context(), analysisdoc.Validate{Lease: lease, ResultJSON: resultJSON}); err != nil {
		t.Fatalf("validate v1: %v", err)
	}
	for range 2 {
		published, err := repo.Publish(t.Context(), lease)
		if err != nil || published.Status != analysisdoc.Published || !published.Active {
			t.Fatalf("publish v1: %+v %v", published, err)
		}
	}
	read, err := repo.ReadJob(t.Context(), analysisdoc.JobRead{JobID: jobID, ReaderID: owner})
	if err != nil || read.ResultJSON != resultJSON || read.Request.AnalysisKind != analysisdoc.KindBriefV1 {
		t.Fatalf("read published v1: %+v %v", read, err)
	}
}
