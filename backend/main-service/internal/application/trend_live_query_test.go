package application

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

func TestLiveQueryPlanRetainsComplementarySubareas(t *testing.T) {
	const response = `{"scope":{"mode":"any","branches":[{"id":"b1","task":"solid-state batteries"}],"required":[]},
		"terms":["solid state batteries", "sulfide electrolytes", "halide electrolytes",
		"oxide electrolytes", "polymer electrolytes", "composite electrolytes", "lithium metal anodes",
		"anode free batteries", "cathode coatings", "interfacial wetting", "solid state sodium",
		"lithium dendrite suppression"]}`
	plan := NewLiveQueryPlanner(fakeGenerator{response}, func(context.Context) bool { return true })
	s := NewTrendLiveService(nil, nil)
	s.SetPlanner(plan)
	s.setWindow([]StudyPaper{{ID: "last-subarea", Title: "Lithium dendrite suppression", Date: "2026-05-10"}}, nil)
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	for _, query := range []string{"твердотельные аккумуляторы", "batteries"} {
		result, err := s.SearchWindow(t.Context(), LiveWindowSearch{Query: query, Periods: periods})
		if err != nil || result.Total != 1 || len(result.Terms) != 12 {
			t.Fatalf("поднаправление в конце плана потерялось при поиске %q: %+v, %v", query, result, err)
		}
		if !slices.Equal(liveScholarPhrases(result.Terms), result.Terms) {
			t.Fatal("журнальный поиск отрезал часть поднаправлений общего плана")
		}
	}
}

func TestQueryPlannerRejectsIncompletePlansAndHonorsBudget(t *testing.T) {
	const valid = `{"terms":["fraud detection"],"scope":{"mode":"any",
		"branches":[{"id":"b1","task":"fraud detection"}],"required":[]}}`
	for _, tc := range []struct {
		name, response, finish string
		parent, delay          time.Duration
		wantErr                bool
	}{
		{name: "complete", response: valid, finish: "stop", parent: 12 * time.Minute, delay: 40 * time.Second},
		{name: "missing_scope", response: `{"terms":["fraud detection"]}`, wantErr: true},
		{name: "legacy_array", response: `["fraud detection"]`, wantErr: true},
		{name: "missing_required", response: strings.Replace(valid, `,"required":[]`, "", 1), wantErr: true},
		{name: "null_required", response: strings.Replace(valid, `"required":[]`, `"required":null`, 1), wantErr: true},
		{name: "bad_mode", response: strings.Replace(valid, `"any"`, `"maybe"`, 1), wantErr: true},
		{name: "truncated", response: valid, finish: "length", wantErr: true},
		{name: "deadline", response: valid, parent: 12 * time.Minute, delay: 61 * time.Second, wantErr: true},
		{name: "parent_canceled", response: valid, parent: 5 * time.Second, delay: 40 * time.Second, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent := tc.parent
				if parent == 0 {
					parent = 12 * time.Minute
				}
				ctx, cancel := context.WithTimeout(t.Context(), parent)
				defer cancel()
				calls := 0
				generator := extractionGenerator{generate: func(ctx context.Context, request aiclients.GenRequest,
				) (aiclients.GenResponse, error) {
					calls++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) != min(parent, 60*time.Second) || request.MaxTokens != 640 {
						t.Fatalf("planner escaped bounded parent: deadline=%v tokens=%d", deadline, request.MaxTokens)
					}
					select {
					case <-ctx.Done():
						return aiclients.GenResponse{}, ctx.Err()
					case <-time.After(tc.delay):
						return aiclients.GenResponse{Text: tc.response, FinishReason: tc.finish}, nil
					}
				}}
				plan, err := NewLiveQueryPlanner(generator, func(context.Context) bool { return true })(ctx, "Антифрод")
				if (err != nil) != tc.wantErr || calls != 1 {
					t.Fatalf("invalid plan or retry: calls=%d plan=%+v err=%v", calls, plan, err)
				}
				if tc.wantErr && plan.Scope != nil {
					t.Fatal("invalid response returned an acceptance scope")
				}
			})
		})
	}
}
