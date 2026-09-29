package postgres_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/storage"
	"github.com/rs/zerolog"
)

type jobLive struct {
	db       *postgres.DB
	repo     *postgres.AnalysisRepo
	store    *storage.Client
	requests []analysisdoc.Request
	keys     map[string]bool
	method   string
}

// Использует только два заранее объявленных входа и никогда не публикует поддельные тренды.
func TestAnalysisJobsLive(t *testing.T) {
	s := newJobLive(t)
	first := s.submit(t, 0, s.method)
	old := s.claim(t, first.MethodSHA256)
	if old.Job.ID != first.ID {
		t.Fatal("wrong pending job")
	}
	second := s.submit(t, 1, s.method)
	current := s.claim(t, second.MethodSHA256)
	s.staleLease(t, old)
	oldRecovered := s.claim(t, first.MethodSHA256)
	if oldRecovered.Job.ID != first.ID || oldRecovered.Lease.Token == old.Lease.Token {
		t.Fatal("expired attempt not recovered")
	}
	oldRaw := s.result(t, oldRecovered)
	_, staleErr := s.repo.Validate(t.Context(), analysisdoc.Validate{Lease: *old.Lease, ResultJSON: oldRaw})
	if !errors.Is(staleErr, analysisdoc.ErrConflict) {
		t.Fatal("old attempt wrote after recovery", staleErr)
	}
	s.put(t, analysisdoc.ResultKey(first.ID, indexdoc.Hash([]byte(oldRaw))), oldRaw)
	if _, err := s.repo.Validate(t.Context(), analysisdoc.Validate{Lease: *oldRecovered.Lease, ResultJSON: oldRaw}); err != nil {
		t.Fatal(err)
	}
	s.assertStatus(t, first.ID, analysisdoc.Validated, false)
	// Процесс может упасть после записи проверенных байтов, но до активации; следующая попытка
	// обязана продолжить с теми же байтами, а не считать замену.
	s.staleLease(t, oldRecovered)
	staged := s.claim(t, first.MethodSHA256)
	if staged.Job.Status != analysisdoc.Validated || staged.Job.ResultJSON != oldRaw {
		t.Fatal("validated recovery changed the saved projection")
	}
	oldRecovered = staged
	s.publishWithMissingObject(t, current)
	s.assertStatus(t, second.ID, analysisdoc.Published, true)
	if _, err := s.repo.Publish(t.Context(), *oldRecovered.Lease); err != nil {
		t.Fatal(err)
	}
	s.assertStatus(t, first.ID, analysisdoc.Published, false)
	s.assertStatus(t, second.ID, analysisdoc.Published, true)
	// Даже новый метод на старом входе не может заменить новые данные.
	method2 := strings.Replace(s.method, "publication-test-v1", "publication-test-v2", 1)
	third := s.submit(t, 0, method2)
	thirdClaim := s.claim(t, third.MethodSHA256)
	raw := s.result(t, thirdClaim)
	s.put(t, analysisdoc.ResultKey(third.ID, indexdoc.Hash([]byte(raw))), raw)
	if _, err := s.repo.Validate(t.Context(), analysisdoc.Validate{Lease: *thirdClaim.Lease, ResultJSON: raw}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.Publish(t.Context(), *thirdClaim.Lease); err != nil {
		t.Fatal(err)
	}
	s.assertStatus(t, third.ID, analysisdoc.Published, false)
	s.assertStatus(t, second.ID, analysisdoc.Published, true)
	s.checkRevokedAndForeign(t, second)
	t.Log("real PostgreSQL/S3: idempotency, concurrent claim, missing object, staged recovery, stale lease, old run, ACL PASS")
}

func newJobLive(t *testing.T) *jobLive {
	t.Helper()
	path := os.Getenv("TR_JOB_MANIFEST")
	if path == "" {
		t.Skip("explicit test input manifest required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &jobLive{keys: map[string]bool{}}
	if err = json.Unmarshal(raw, &s.requests, json.RejectUnknownMembers(true)); err != nil || len(s.requests) != 2 {
		t.Fatal("two requests required", err)
	}
	for _, r := range s.requests {
		if _, err = r.Normalize(time.Now()); err != nil || r.TestRunID == "" {
			t.Fatal("test-scoped inputs required", err)
		}
	}
	if s.requests[0].TestRunID != s.requests[1].TestRunID {
		t.Fatal("one test namespace required")
	}
	s.db, err = postgres.Connect(t.Context(), os.Getenv("POSTGRES_DSN"), zerolog.Nop())
	if err != nil {
		t.Fatal("existing PostgreSQL unavailable")
	}
	t.Cleanup(s.db.Close)
	s.store, err = storage.Open(t.Context(), storage.ConfigFromEnv())
	if err != nil {
		t.Fatal("existing S3 configuration unavailable")
	}
	s.repo = postgres.NewAnalysisJobs(s.db, s.store)
	var n int
	if err = s.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM signal_analysis_inputs WHERE request_id=ANY($1)`,
		[]string{s.requests[0].RequestID, s.requests[1].RequestID}).Scan(&n); err != nil || n != 0 {
		t.Fatal("IDs already used", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, r := range s.requests {
			if _, cleanupErr := s.db.Pool.Exec(ctx, `DELETE FROM signal_analysis_inputs WHERE owner_id=$1 AND request_id=$2
AND manifest::jsonb->'request'->>'test_run_id'=$3`, r.OwnerID, r.RequestID, r.TestRunID); cleanupErr != nil {
				t.Error(cleanupErr)
			}
		}
		for key := range s.keys {
			if cleanupErr := s.store.Delete(ctx, key); cleanupErr != nil {
				t.Error(cleanupErr)
			}
		}
	})
	m := analysisdoc.Method{Version: "publication-test-v1", QualityStatus: "not_frozen",
		CodeSHA256: indexdoc.Hash([]byte(s.requests[0].TestRunID)),
		// Опубликованные прогоны называют версию каждой части, поэтому их объявляет и фикстура.
		Parameters: jsontext.Value(`{"test_only":true,"brief_version":"test-brief-v1",` +
			`"candidates_version":"test-candidates-v1","emergence_version":"test-emergence-v1",` +
			`"versions":{"identity":"test-identity-v1","measurements":"test-measurements-v1"}}`),
		Explanation: jsontext.Value(`{"meaning":"publication state acceptance, not a measured trend"}`)}
	method, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	s.method = string(method)
	return s
}

func (s *jobLive) put(t *testing.T, key, raw string) {
	t.Helper()
	s.keys[key] = true
	if err := s.store.Put(t.Context(), key, strings.NewReader(raw), int64(len(raw)), "application/json"); err != nil {
		t.Fatal(err)
	}
}

func (s *jobLive) submit(t *testing.T, i int, method string) analysisdoc.Job {
	t.Helper()
	frozen, err := s.repo.Freeze(t.Context(), s.requests[i])
	if err != nil {
		t.Fatal(err)
	}
	s.put(t, frozen.Artifact, frozen.JSON)
	s.put(t, analysisdoc.MethodKey(indexdoc.Hash([]byte(method))), method)
	in := analysisdoc.Submit{Read: analysisdoc.Read{RunID: frozen.RunID,
		ReaderID: s.requests[i].OwnerID, SharedCorpus: true}, MethodJSON: method}
	var group sync.WaitGroup
	jobs := make([]analysisdoc.Job, 3)
	errs := make([]error, 3)
	for j := range 3 {
		group.Go(func() { jobs[j], errs[j] = s.repo.Submit(t.Context(), in) })
	}
	group.Wait()
	for j := range 3 {
		if errs[j] != nil || jobs[j].ID != jobs[0].ID {
			t.Fatal("submit retry diverged", errs)
		}
	}
	return jobs[0]
}

func (s *jobLive) claim(t *testing.T, method string) analysisdoc.Claimed {
	t.Helper()
	var group sync.WaitGroup
	claims := make([]analysisdoc.Claimed, 3)
	errs := make([]error, 3)
	for i := range 3 {
		group.Go(func() {
			claims[i], errs[i] = s.repo.Claim(t.Context(), analysisdoc.Claim{
				WorkerID: uuid.NewV4().String(), MethodSHA256: method})
		})
	}
	group.Wait()
	var got analysisdoc.Claimed
	n := 0
	for i, c := range claims {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if c.Job != nil {
			got = c
			n++
		}
	}
	if n != 1 {
		t.Fatal("one job must have exactly one claimant", n)
	}
	return got
}

func (s *jobLive) staleLease(t *testing.T, c analysisdoc.Claimed) {
	t.Helper()
	if _, err := s.db.Pool.Exec(t.Context(), `UPDATE signal_analysis_jobs SET lease_until=now()-interval '1 second'
WHERE id=$1 AND lease_token=$2`, c.Job.ID, c.Lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.Renew(t.Context(), *c.Lease); !errors.Is(err, analysisdoc.ErrConflict) {
		t.Fatal("expired renewal accepted", err)
	}
}

func (s *jobLive) result(t *testing.T, c analysisdoc.Claimed) string {
	t.Helper()
	input, err := analysisdoc.Decode[analysisdoc.Input](c.Input.JSON)
	if err != nil || len(input.Records) == 0 || input.Model == nil {
		t.Fatal("publication acceptance requires real ready sources", err)
	}
	t.Logf("publication input: %d ready records, %d exclusions, %d coverage pages",
		len(input.Records), len(input.Excluded), len(input.Pages))
	r := analysisdoc.Result{Version: analysisdoc.ResultVersion, JobID: c.Job.ID, InputRunID: input.RunID,
		InputSHA256: c.Input.SHA256, MethodSHA256: c.Job.MethodSHA256, Query: input.Request.Query, Cutoff: input.Request.Cutoff,
		Coverage: jsontext.Value(`{"status":"publication_test_only"}`),
		Ranking:  analysisdoc.Ranking{Completeness: "insufficient_coverage", Reasons: []string{"test_does_not_run_method"}}}
	for _, d := range input.Records {
		id := "publication-test:" + d.ID
		r.Works = append(r.Works, analysisdoc.Work{ID: id, Kind: d.Source.Kind, DocumentIDs: []string{d.ID}})
		r.Unassigned = append(r.Unassigned, id)
	}
	raw, err := analysisdoc.MarshalResult(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (s *jobLive) assertStatus(t *testing.T, id, state string, active bool) {
	t.Helper()
	j, err := s.repo.ReadJob(t.Context(), analysisdoc.JobRead{JobID: id, ReaderID: s.requests[0].OwnerID, SharedCorpus: true})
	if err != nil || j.Status != state || j.Active != active || (state != analysisdoc.Published && j.ResultJSON != "") {
		t.Fatal("partial or wrong active result exposed", err, j.Status, j.Active)
	}
}

func (s *jobLive) publishWithMissingObject(t *testing.T, c analysisdoc.Claimed) {
	t.Helper()
	raw := s.result(t, c)
	key := analysisdoc.ResultKey(c.Job.ID, indexdoc.Hash([]byte(raw)))
	in := analysisdoc.Validate{Lease: *c.Lease, ResultJSON: raw}
	if _, err := s.repo.Validate(t.Context(), in); err == nil {
		t.Fatal("missing result object validated")
	}
	s.put(t, key, raw)
	if _, err := s.repo.Validate(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.Publish(t.Context(), *c.Lease); err == nil {
		t.Fatal("disappeared object activated")
	}
	s.assertStatus(t, c.Job.ID, analysisdoc.Validated, false)
	s.put(t, key, raw)
	first, err := s.repo.Publish(t.Context(), *c.Lease)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.repo.Publish(t.Context(), *c.Lease)
	if err != nil || first.ResultSHA256 != again.ResultSHA256 || !again.Active {
		t.Fatal("publish retry diverged", err)
	}
}

func (s *jobLive) checkRevokedAndForeign(t *testing.T, j analysisdoc.Job) {
	t.Helper()
	read := analysisdoc.JobRead{JobID: j.ID, ReaderID: uuid.NewV4().String(), SharedCorpus: true}
	if _, err := s.repo.ReadJob(t.Context(), read); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("foreign aggregate leaked", err)
	}
	missing := uuid.NewV4().String()
	if _, err := s.db.Pool.Exec(t.Context(), `INSERT INTO signal_analysis_sources(run_id,document_id,snapshot_id)
VALUES($1,$2,'')`, j.InputRunID, missing); err != nil {
		t.Fatal(err)
	}
	read.ReaderID = s.requests[0].OwnerID
	if _, err := s.repo.ReadJob(t.Context(), read); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("revoked input remained visible", err)
	}
	if _, err := s.db.Pool.Exec(t.Context(), `DELETE FROM signal_analysis_sources WHERE run_id=$1 AND document_id=$2`,
		j.InputRunID, missing); err != nil {
		t.Fatal(err)
	}
	s.assertStatus(t, j.ID, analysisdoc.Published, true)
}
