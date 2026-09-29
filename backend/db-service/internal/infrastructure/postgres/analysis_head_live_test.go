package postgres_test

import (
	"strings"
	"testing"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
)

func TestAnalysisEvaluatedHeadLive(t *testing.T) {
	s := newJobLive(t)
	// Метка evaluated только для теста и только для этого заранее объявленного прогона.
	evaluated := strings.Replace(s.method, `"not_frozen"`, `"evaluated"`, 1)
	first := s.submit(t, 0, evaluated)
	publishHeadTest(t, s, first)
	s.assertStatus(t, first.ID, analysisdoc.Published, true)
	for _, method := range []string{s.method, evaluated} {
		newer := s.submit(t, 1, method)
		publishHeadTest(t, s, newer)
		s.assertStatus(t, newer.ID, analysisdoc.Published, false)
		s.assertStatus(t, first.ID, analysisdoc.Published, true)
	}
	t.Log("experimental method and incomplete evaluated result remain readable without replacing evaluated head: PASS")
}

func publishHeadTest(t *testing.T, s *jobLive, job analysisdoc.Job) {
	t.Helper()
	claim := s.claim(t, job.MethodSHA256)
	if claim.Job.ID != job.ID {
		t.Fatal("wrong test job claimed")
	}
	raw := s.result(t, claim)
	s.put(t, analysisdoc.ResultKey(job.ID, indexdoc.Hash([]byte(raw))), raw)
	if _, err := s.repo.Validate(t.Context(), analysisdoc.Validate{Lease: *claim.Lease, ResultJSON: raw}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.Publish(t.Context(), *claim.Lease); err != nil {
		t.Fatal(err)
	}
}
