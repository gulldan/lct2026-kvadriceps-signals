package postgres_test

import (
	"encoding/json/jsontext"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
)

// Трогает только два заранее объявленных входа из TR_JOB_MANIFEST.
func TestAnalysisDescriptionsLive(t *testing.T) {
	s := newJobLive(t)
	job := s.submit(t, 0, s.method)
	claimed := s.claim(t, job.MethodSHA256)
	raw := s.descriptionResult(t, claimed)
	s.put(t, analysisdoc.ResultKey(job.ID, indexdoc.Hash([]byte(raw))), raw)
	if _, err := s.repo.Validate(t.Context(), analysisdoc.Validate{Lease: *claimed.Lease, ResultJSON: raw}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.Publish(t.Context(), *claimed.Lease); err != nil {
		t.Fatal(err)
	}
	read := analysisdoc.DescriptionRead{JobID: job.ID, ReaderID: s.requests[0].OwnerID,
		SharedCorpus: true, CandidateID: "test-topic-0"}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := s.repo.StartDescription(t.Context(), read); errorsCh <- err })
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal("idempotent concurrent start", err)
		}
	}
	read.CandidateID = "test-topic-1"
	if _, err := s.repo.StartDescription(t.Context(), read); err != nil {
		t.Fatal(err)
	}
	read.CandidateID = "test-topic-2"
	if _, err := s.repo.StartDescription(t.Context(), read); !errors.Is(err, analysisdoc.ErrCapacity) {
		t.Fatal("owner queue not bounded", err)
	}
	claim := func() analysisdoc.DescriptionClaimed {
		t.Helper()
		c, err := s.repo.ClaimDescription(t.Context(), analysisdoc.DescriptionClaim{
			Version: analysisdoc.DescriptionVersion, TestRunID: s.requests[0].TestRunID})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := claim()
	if first.Lease == nil || first.Lease.CandidateID != "test-topic-0" {
		t.Fatal("wrong test claim")
	}
	s.expireDescription(t, *first.Lease)
	recovered := claim()
	if recovered.Lease == nil || recovered.Lease.Token == first.Lease.Token {
		t.Fatal("no fenced recovery")
	}
	old := analysisdoc.DescriptionComplete{DescriptionLease: *first.Lease, Result: first.Bundle.Template("model_unavailable")}
	if _, err := s.repo.CompleteDescription(t.Context(), old); !errors.Is(err, analysisdoc.ErrConflict) {
		t.Fatal("late attempt accepted", err)
	}
	d := recovered.Bundle.Template("model_unavailable")
	d.Fields.Problem = &analysisdoc.BriefField{Text: "This proves a 10x production advantage",
		EvidenceIDs: recovered.Bundle.Fields.Problem.EvidenceIDs}
	d.Status = "validated"
	d.Reason = ""
	d.Model = "test-model"
	completion := analysisdoc.DescriptionComplete{DescriptionLease: *recovered.Lease, Result: d}
	if _, err := s.repo.CompleteDescription(t.Context(), completion); !errors.Is(err, analysisdoc.ErrInvalid) {
		t.Fatal("false cited inference accepted", err)
	}
	d.Fields = recovered.Bundle.Fields
	d.Fields.Problem = &analysisdoc.BriefField{Text: analysisdoc.DescriptionPrefix + recovered.Bundle.Fields.Problem.Text + "»",
		EvidenceIDs: recovered.Bundle.Fields.Problem.EvidenceIDs}
	completion.Result = d
	completed, err := s.repo.CompleteDescription(t.Context(), completion)
	if err != nil || completed.Status != "validated" {
		t.Fatal("valid source attribution failed", err)
	}
	if _, err = s.repo.CompleteDescription(t.Context(), completion); err != nil {
		t.Fatal("completion replay", err)
	}
	read.CandidateID = "test-topic-0"
	read.ReaderID = uuid.NewV4().String()
	if _, err = s.repo.ReadDescription(t.Context(), read); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("foreign prose leaked", err)
	}
	read.ReaderID = s.requests[0].OwnerID
	missing := uuid.NewV4().String()
	if _, err = s.db.Pool.Exec(t.Context(), `INSERT INTO signal_analysis_sources(run_id,document_id,snapshot_id)
VALUES($1,$2,'')`, job.InputRunID, missing); err != nil {
		t.Fatal(err)
	}
	if _, err = s.repo.ReadDescription(t.Context(), read); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("revoked aggregate prose leaked", err)
	}
	if _, err = s.db.Pool.Exec(t.Context(), `DELETE FROM signal_analysis_sources WHERE run_id=$1 AND document_id=$2`,
		job.InputRunID, missing); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		c := claim()
		if c.Lease == nil || c.Lease.CandidateID != "test-topic-1" {
			t.Fatal("wrong retry claim")
		}
		s.expireDescription(t, *c.Lease)
	}
	if c := claim(); c.Lease != nil {
		t.Fatal("fourth model attempt admitted")
	}
	read.CandidateID = "test-topic-1"
	fallback, err := s.repo.ReadDescription(t.Context(), read)
	if err != nil || fallback.Status != "template" || fallback.Result.Reason != "attempts_exhausted" {
		t.Fatal("crash fallback", err)
	}
	read.CandidateID = "test-topic-2"
	if _, err = s.repo.StartDescription(t.Context(), read); err != nil {
		t.Fatal("capacity not released", err)
	}
	if c := claim(); c.Lease != nil {
		t.Fatal("model invoked for unknown facts")
	}
	unchanged, err := s.repo.ReadJob(t.Context(), read.JobRead)
	if err != nil || unchanged.ResultJSON != raw {
		t.Fatal("description changed published analysis", err)
	}
	t.Logf("description queue PASS: idempotency, capacity, lease recovery, numeric gate, ACL, fallback; namespace=%s; job=%s",
		s.requests[0].TestRunID, job.ID)
}

func (s *jobLive) expireDescription(t *testing.T, lease analysisdoc.DescriptionLease) {
	t.Helper()
	count, err := s.db.Pool.Exec(t.Context(), `UPDATE signal_descriptions SET lease_until=now()-interval '1 second'
WHERE job_id=$1 AND candidate_id=$2 AND version=$3 AND lease_token=$4`,
		lease.JobID, lease.CandidateID, lease.Version, lease.Token)
	if err != nil || count.RowsAffected() != 1 {
		t.Fatal("expire exact test lease", err)
	}
}

func (s *jobLive) descriptionResult(t *testing.T, c analysisdoc.Claimed) string {
	t.Helper()
	r, err := analysisdoc.Decode[analysisdoc.Result](s.result(t, c))
	if err != nil {
		t.Fatal(err)
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](c.Input.JSON)
	if err != nil || len(input.Records) < 6 {
		t.Fatal("six actual sources required", err)
	}
	for i, id := range []string{"test-topic-0", "test-topic-1", "test-topic-2"} {
		candidate := analysisdoc.Candidate{ID: id, Title: "Queue acceptance candidate, not a technology claim",
			WorkIDs:      []string{r.Works[i*2].ID, r.Works[i*2+1].ID},
			Measurements: jsontext.Value(`{"test_only":true}`), Assessment: jsontext.Value(`{"eligible":false}`),
			Brief:         analysisdoc.Brief{DescriptionStatus: "template", EvidenceSHA256: strings.Repeat("a", 64)},
			Relevance:     analysisdoc.Relevance{Version: "test-only", Mode: "lexical_fallback"},
			MissingFields: []string{"temporal_basis", "advantage", "example"}}
		if i < 2 {
			doc := input.Records[i*2]
			body, openErr := s.store.Get(t.Context(), doc.Index.Text.Key)
			if openErr != nil {
				t.Fatal(openErr)
			}
			bytes, readErr := io.ReadAll(io.LimitReader(body, 16<<20))
			_ = body.Close()
			if readErr != nil || indexdoc.Hash(bytes) != doc.Index.Text.SHA256 {
				t.Fatal("pinned source unavailable", readErr)
			}
			runes := []rune(string(bytes))
			quote := string(runes[:min(200, len(runes))])
			e := analysisdoc.Evidence{ID: indexdoc.Hash([]byte(id + quote)), Field: "problem",
				WorkID: candidate.WorkIDs[0], DocumentID: doc.ID, SnapshotID: doc.Index.SnapshotID,
				SourceRevision: doc.Index.SourceRevision, TextSHA256: doc.Index.Text.SHA256,
				End: len([]rune(quote)), Quote: quote, ClaimType: "research_claim"}
			candidate.Evidence = []analysisdoc.Evidence{e}
			candidate.Brief.Problem = &analysisdoc.BriefField{Text: quote, EvidenceIDs: []string{e.ID}}
		}
		r.Candidates = append(r.Candidates, candidate)
	}
	r.Unassigned = r.Unassigned[6:]
	raw, err := analysisdoc.MarshalResult(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
