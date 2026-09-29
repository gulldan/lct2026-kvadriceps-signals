package analysisdoc_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/example/auth/internal/platform/analysisdoc"
)

func technologyFixture(t *testing.T) (analysisdoc.Result, analysisdoc.Job, analysisdoc.Input) {
	t.Helper()
	job := analysisdoc.Job{ID: "job-1", InputSHA256: strings.Repeat("a", 64), MethodSHA256: strings.Repeat("b", 64)}
	input := analysisdoc.Input{RunID: "run-1", Records: []analysisdoc.Record{{ID: "doc-1"}, {ID: "doc-2"}}}
	input.Request = analysisdoc.Request{Cutoff: "2026-01-01T00:00:00Z", Mode: analysisdoc.ModeAsOf}
	result := analysisdoc.Result{
		Version: analysisdoc.ResultVersion, JobID: job.ID, InputRunID: input.RunID,
		InputSHA256: job.InputSHA256, MethodSHA256: job.MethodSHA256, Cutoff: input.Request.Cutoff,
		Coverage:   jsontext.Value(`{"test_only":true}`),
		Works:      []analysisdoc.Work{{ID: "w-1", DocumentIDs: []string{"doc-1"}}, {ID: "w-2", DocumentIDs: []string{"doc-2"}}},
		Unassigned: []string{"w-1", "w-2"},
		Ranking:    analysisdoc.Ranking{Completeness: "insufficient_coverage", Reasons: []string{"test_only"}},
		Technologies: []analysisdoc.ResultTechnology{
			{TechnologyID: "t-1", Name: "Мембранный электролизёр", Role: "proposed_contribution",
				Evidence: []analysisdoc.ResultQuote{
					{DocumentID: "doc-1", Start: 0, End: 10, Quote: "электролиз"},
					{DocumentID: "doc-2", Start: 5, End: 15, Quote: "мембрана"},
				},
				Support: analysisdoc.TechnologySupport{Numerator: 2, Denominator: 2}},
			{TechnologyID: "t-2", Name: "Перовскитный модуль", Role: "evaluated_application",
				Evidence: []analysisdoc.ResultQuote{{DocumentID: "doc-1", Start: 20, End: 30, Quote: "перовскит"}},
				Support:  analysisdoc.TechnologySupport{Numerator: 1, Denominator: 2}},
		},
		Directions: []analysisdoc.ResultDirection{
			{DirectionID: "d-1", Title: "Водород", Technologies: []string{"t-1", "t-2"}, Stage: "early_signal"},
		},
		TechnologyOrder: analysisdoc.ResultTechnologyOrder{Rule: analysisdoc.OrderBySupport,
			OrderedIDs: []string{"t-1", "t-2"}},
	}
	return result, job, input
}

func encode(t *testing.T, result analysisdoc.Result) string {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTechnologyResultIsCheckedNotTrusted(t *testing.T) {
	good, job, input := technologyFixture(t)
	if err := analysisdoc.ValidateResult(encode(t, good), job, input); err != nil {
		t.Fatalf("an honest result was refused: %v", err)
	}

	for name, tamper := range map[string]func(*analysisdoc.Result){
		"unsupported technology without quotes": func(r *analysisdoc.Result) {
			r.Technologies[1].Evidence = nil
			r.Technologies[1].Support.Numerator = 0
		},
		"order silently omits strongest result": func(r *analysisdoc.Result) {
			r.TechnologyOrder.OrderedIDs = []string{"t-2"}
		},
		"order the declared rule does not produce": func(r *analysisdoc.Result) {
			r.TechnologyOrder.OrderedIDs = []string{"t-2", "t-1"}
		},
		"order rule nobody declared": func(r *analysisdoc.Result) {
			r.TechnologyOrder.Rule = "whatever-looks-best"
		},
		"a quote of a revision outside the manifest": func(r *analysisdoc.Result) {
			r.Technologies[0].Evidence[0].DocumentID = "doc-99"
		},
		"support inflated": func(r *analysisdoc.Result) {
			r.Technologies[1].Support.Numerator = 2
		},
		"denominator narrowed": func(r *analysisdoc.Result) {
			r.Technologies[0].Support.Denominator = 1
		},
		"a direction naming a technology the run did not produce": func(r *analysisdoc.Result) {
			r.Directions[0].Technologies = []string{"t-1", "t-404"}
		},
		"a direction with no members": func(r *analysisdoc.Result) {
			r.Directions[0].Technologies = nil
		},
		"an empty quote": func(r *analysisdoc.Result) {
			r.Technologies[0].Evidence[0].Quote = "   "
		},
		"a result of another job": func(r *analysisdoc.Result) { r.JobID = "job-2" },
	} {
		broken, _, _ := technologyFixture(t)
		tamper(&broken)
		if err := analysisdoc.ValidateResult(encode(t, broken), job, input); err == nil {
			t.Fatalf("a tampered result was accepted: %s", name)
		}
	}

	if err := analysisdoc.ValidateResult(`{"version":"trend-result-v1"}`, job, input); err == nil {
		t.Fatal("an incomplete envelope was accepted")
	}
}

func TestHistoryModeOfAResultAcceptsTheOldWord(t *testing.T) {
	result, job, input := technologyFixture(t)
	input.Request.Mode = "as_observed"
	if err := analysisdoc.ValidateResult(encode(t, result), job, input); err != nil {
		t.Fatalf("a run frozen under the old word was refused: %v", err)
	}
}
