package application

import (
	"context"
	"errors"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestJobs_Execute_Dispatch(t *testing.T) {
	cat := newCatalog()
	cat.kpis["kpi1"] = &commonv1.KPI{Id: "kpi1", OwnerId: "alice", Title: "T"}
	cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "alpha beta gamma delta", Title: "t"})
	a := newAnswerer(reply(genArray), reply("[]"))
	svc := jobService(t, cat, a, newJobStore())

	ids, err := svc.execute(context.Background(), "alice", HypothesisJobGenerate, HypothesisJobInput{KPIID: "kpi1", Count: 1})
	if err != nil {
		t.Fatalf("execute generate: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("generate must return created ids")
	}

	if _, err := svc.execute(context.Background(), "alice", "bogus", HypothesisJobInput{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unknown kind must be invalid, got %v", err)
	}
}

func TestJobs_Execute_SingleHypothesisKinds(t *testing.T) {
	verdict := `{"verdict":"supported","confidence":0.9,"rationale":"",` +
		`"claims":[{"claim":"alpha beta gamma delta","verdict":"supported","quote":"alpha beta gamma delta"}],` +
		`"supporting":["alpha beta gamma delta"],"contradicting":[]}`
	trl := `{"levels":[{"level":1,"met":false}]}`
	comp := `{"summary":"s","competitors":[]}`
	tag := `{"research_type":"практическое","grnti":[],"vak":[],"asjc":[]}`
	cases := []struct {
		kind   string
		answer string
	}{
		{HypothesisJobVerify, verdict},
		{HypothesisJobAssessTRL, trl},
		{HypothesisJobCompetitors, comp},
		{HypothesisJobRefine, verdict},
		{HypothesisJobTag, tag},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			cat := newCatalog()
			cat.putHypothesis(&commonv1.Hypothesis{
				Id: "h1", OwnerId: "alice", Title: "t", Statement: "alpha beta gamma delta",
			})
			svc := jobService(t, cat, newAnswerer(reply(tc.answer)), newJobStore())
			ids, err := svc.execute(context.Background(), "alice", tc.kind, HypothesisJobInput{HypothesisID: "h1"})
			if err != nil {
				t.Fatalf("execute %s: %v", tc.kind, err)
			}
			if len(ids) != 1 || ids[0] != "h1" {
				t.Fatalf("execute %s ids = %v, want [h1]", tc.kind, ids)
			}
		})
	}
}

func TestJobs_Execute_GenerateInlineKPI(t *testing.T) {
	cat := newCatalog()
	a := newAnswerer(reply(genArray), reply("[]"))
	svc := jobService(t, cat, a, newJobStore())
	ids, err := svc.execute(context.Background(), "alice", HypothesisJobGenerate,
		HypothesisJobInput{KPITitle: "New KPI", Count: 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("inline-KPI generate must produce hypotheses")
	}
}

func TestEnrichmentAbstentionFailsWithoutChangingPassport(t *testing.T) {
	cat := newCatalog()
	cat.putHypothesis(&commonv1.Hypothesis{
		Id: "h1", OwnerId: "alice", Statement: "Unverified effect", Status: "generated", Detail: `{"existing":"keep"}`,
	})
	answerer := newAnswerer(answer{resp: &commonv1.RagResponse{
		Answer: "Недостаточно контекста.", Trace: &commonv1.RagTrace{AbstainReason: "no_sources"},
	}})
	store := newJobStore()
	svc := NewHypothesisJobService(enrichService(cat, answerer, nil), store)
	job := &HypothesisJob{
		ID: "enrich-no-context", OwnerID: "alice", Kind: HypothesisJobEnrich, Token: "lease",
		Input: HypothesisJobInput{HypothesisID: "h1"},
	}
	svc.run(t.Context(), job)
	saved, err := store.GetHypothesisJob(t.Context(), "alice", job.ID)
	if err != nil || saved.Status != HypothesisJobFailed || saved.Error != "insufficient_context" || len(saved.ResultIDs) != 0 {
		t.Fatalf("abstention job outcome: %+v, %v", saved, err)
	}
	if cat.updateCalls != 0 || cat.hypotheses["h1"].GetDetail() != `{"existing":"keep"}` ||
		cat.hypotheses["h1"].GetStatus() != "generated" || enrichmentDone(cat.hypotheses["h1"].GetAssessment()) {
		t.Fatal("abstention wrote a passport, success marker or archived the hypothesis")
	}
}
