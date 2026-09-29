package application

import (
	"context"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestJobs_EnsureVerify(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "missing", OwnerId: "alice", Title: "Missing", Statement: "alpha beta gamma delta",
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "done", OwnerId: "alice", Title: "Done", Statement: "alpha beta",
		Assessment: `{"check":{"verdict":"supported"}}`,
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "foreign", OwnerId: "bob", Title: "Foreign", Statement: "alpha beta",
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "closed", OwnerId: "alice", Title: "Closed", Status: "archived", Statement: "alpha beta",
	})
	verdict := reply(`{"verdict":"supported","confidence":0.9,"rationale":"ok",`+
		`"claims":[{"claim":"alpha beta gamma delta","verdict":"supported","quote":"alpha beta"}],`+
		`"supporting":["alpha beta"],"contradicting":[]}`,
		&commonv1.Source{ChunkId: "c1", Snippet: "alpha beta gamma delta epsilon"})
	svc := jobService(t, cat, newAnswerer(verdict), newJobStore())

	started := svc.EnsureVerify(context.Background(), "alice", []*commonv1.Hypothesis{
		cat.hypotheses["missing"],
		cat.hypotheses["done"],
		cat.hypotheses["foreign"],
		cat.hypotheses["closed"],
	}, 10)
	if started != 1 {
		t.Fatalf("EnsureVerify started %d jobs, want 1", started)
	}
	jobs, err := svc.List(context.Background(), "alice", 10)
	if err != nil {
		t.Fatalf("List jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Kind != HypothesisJobVerify {
		t.Fatalf("expected one verify job, got %+v", jobs)
	}
	done := waitJob(t, svc, jobs[0].ID)
	if done.Status != HypothesisJobSucceeded {
		t.Fatalf("auto verify job status = %s (%s)", done.Status, done.Error)
	}
	if got := verdictFromAssessment(cat.hypotheses["missing"].GetAssessment()); got != verdictSupported {
		t.Fatalf("missing hypothesis must be verified, got verdict %q", got)
	}
	if again := svc.EnsureVerify(context.Background(), "alice", []*commonv1.Hypothesis{cat.hypotheses["missing"]}, 10); again != 0 {
		t.Fatalf("EnsureVerify must not enqueue a duplicate, got %d", again)
	}
}

func TestJobs_EnsureAssessTRL(t *testing.T) {
	cat := newCatalog()
	withTRL := int32(4)
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "missing", OwnerId: "alice", Title: "Missing", Statement: "alpha beta gamma delta",
		Assessment: `{"check":{"verdict":"supported"}}`,
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "done", OwnerId: "alice", Title: "Done", Statement: "alpha beta", Trl: &withTRL,
		Assessment: `{"check":{"verdict":"supported"}}`,
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "unverified", OwnerId: "alice", Title: "Unverified", Statement: "alpha beta",
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "foreign", OwnerId: "bob", Title: "Foreign", Statement: "alpha beta",
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "closed", OwnerId: "alice", Title: "Closed", Status: "archived", Statement: "alpha beta",
	})
	a := newAnswerer(reply(`{"levels":[{"level":1,"met":true},{"level":2,"met":false}]}`))
	svc := jobService(t, cat, a, newJobStore())

	started := svc.EnsureAssessTRL(context.Background(), "alice", []*commonv1.Hypothesis{
		cat.hypotheses["missing"],
		cat.hypotheses["done"],
		cat.hypotheses["unverified"],
		cat.hypotheses["foreign"],
		cat.hypotheses["closed"],
	}, 10)
	if started != 1 {
		t.Fatalf("EnsureAssessTRL started %d jobs, want 1", started)
	}
	jobs, err := svc.List(context.Background(), "alice", 10)
	if err != nil {
		t.Fatalf("List jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Kind != HypothesisJobAssessTRL {
		t.Fatalf("expected one assess_trl job, got %+v", jobs)
	}
	done := waitJob(t, svc, jobs[0].ID)
	if done.Status != HypothesisJobSucceeded {
		t.Fatalf("auto TRL job status = %s (%s)", done.Status, done.Error)
	}
	if cat.hypotheses["missing"].Trl == nil || cat.hypotheses["missing"].GetTrl() != 1 {
		t.Fatalf("missing hypothesis must be scored to УГТ 1, got %v", cat.hypotheses["missing"].Trl)
	}
	again := svc.EnsureAssessTRL(context.Background(), "alice", []*commonv1.Hypothesis{cat.hypotheses["missing"]}, 10)
	if again != 0 {
		t.Fatalf("EnsureAssessTRL must not enqueue a duplicate, got %d", again)
	}
}

func TestJobs_EnsureEnrich(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "enrichable", OwnerId: "alice", Title: "E", Statement: "alpha beta gamma", Status: "generated",
		Assessment: `{"check":{"verdict":"supported"}}`,
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "unverified", OwnerId: "alice", Title: "U", Statement: "delta epsilon",
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "enriched", OwnerId: "alice", Title: "D", Statement: "zeta eta",
		Assessment: `{"check":{"verdict":"supported"},"enrichment":{"enriched_at":"2026-01-01T00:00:00Z"}}`,
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "closed", OwnerId: "alice", Title: "C", Status: "archived", Statement: "theta",
		Assessment: `{"check":{"verdict":"supported"}}`,
	})
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "foreign", OwnerId: "bob", Title: "F", Statement: "iota",
		Assessment: `{"check":{"verdict":"supported"}}`,
	})
	enrichReply := reply(`{"microstructure_mechanism":"м","causal_chain":[{"stage":"процесс","change":"x"}],` +
		`"experiment_plan":{"success_criteria":"крит"}}`)
	svc := jobService(t, cat, newAnswerer(enrichReply), newJobStore())

	started := svc.EnsureEnrich(context.Background(), "alice", []*commonv1.Hypothesis{
		cat.hypotheses["enrichable"],
		cat.hypotheses["unverified"],
		cat.hypotheses["enriched"],
		cat.hypotheses["closed"],
		cat.hypotheses["foreign"],
	}, 10)
	if started != 1 {
		t.Fatalf("EnsureEnrich started %d jobs, want 1", started)
	}
	jobs, err := svc.List(context.Background(), "alice", 10)
	if err != nil {
		t.Fatalf("List jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Kind != HypothesisJobEnrich {
		t.Fatalf("expected one enrich job, got %+v", jobs)
	}
	done := waitJob(t, svc, jobs[0].ID)
	if done.Status != HypothesisJobSucceeded {
		t.Fatalf("auto enrich job status = %s (%s)", done.Status, done.Error)
	}
	if !enrichmentDone(cat.hypotheses["enrichable"].GetAssessment()) {
		t.Fatal("enrichable hypothesis must carry the enrichment marker after the job")
	}
	one := []*commonv1.Hypothesis{cat.hypotheses["enrichable"]}
	if again := svc.EnsureEnrich(context.Background(), "alice", one, 10); again != 0 {
		t.Fatalf("EnsureEnrich must not enqueue a duplicate, got %d", again)
	}
}
