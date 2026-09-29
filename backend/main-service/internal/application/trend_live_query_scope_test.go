package application

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Одиночная задача задана фикстурой явно; это не запасной разбор запроса в рабочем коде.
func liveFixtureQueryScope(query string) *LiveQueryScope {
	normalized, _ := normalizeLiveQuery(query)
	return &LiveQueryScope{Query: normalized, Mode: "any",
		Branches: []LiveScopeBranch{{ID: "b1", Task: "Задача: " + query}}, Required: []LiveScopeCondition{}}
}

func liveFixtureScopeContext(ctx context.Context, query string) context.Context {
	return liveWithQueryScope(ctx, liveFixtureQueryScope(query))
}

func liveFixtureQueryPlan(query string, terms []string) LiveQueryPlan {
	return LiveQueryPlan{Terms: terms, Scope: liveFixtureQueryScope(query)}
}

func liveTestQueryPlanner(translate QueryTranslator) LiveQueryPlanner {
	return func(ctx context.Context, query string) (LiveQueryPlan, error) {
		terms, err := translate(ctx, query)
		if err != nil {
			return LiveQueryPlan{}, err
		}
		return liveFixtureQueryPlan(query, terms), nil
	}
}

func TestQueryScopeIsImmutableAndBoundToOriginalQuery(t *testing.T) {
	const query = "Антифрод и биометрия в банковских платежах"
	scope := liveFixtureQueryScope(query)
	ctx := liveWithQueryScope(t.Context(), scope)
	scope.Branches[0].Task = "Другая задача"
	first, err := liveQueryScope(ctx, query)
	if err != nil || first.Branches[0].Task != "Задача: "+query {
		t.Fatalf("caller mutated the stored scope: %+v, %v", first, err)
	}
	first.Branches[0].Task = "Ещё одна задача"
	second, err := liveQueryScope(ctx, query)
	if err != nil || second.Branches[0].Task != "Задача: "+query {
		t.Fatalf("reader mutated the stored scope: %+v, %v", second, err)
	}
	if _, err := liveQueryScope(ctx, "Другая задача"); err == nil {
		t.Fatal("scope leaked into another query")
	}
	if _, err := liveQueryScope(t.Context(), query); err == nil {
		t.Fatal("missing plan became an implicit acceptance scope")
	}
}

func liveScopedDetailFixture(query string, detail LiveDetail) LiveDetail {
	detail.Scope = liveFixtureQueryScope(query)
	detail.Candidates = slices.Clone(detail.Candidates)
	for i := range detail.Candidates {
		if detail.Candidates[i].Qualification == nil {
			continue
		}
		qualification := *detail.Candidates[i].Qualification
		qualification.Scope = liveCloneQueryScope(detail.Scope)
		qualification.Evidence = slices.Clone(qualification.Evidence)
		for j := range qualification.Evidence {
			qualification.Evidence[j].ScopeCriteria = []string{"b1"}
		}
		detail.Candidates[i].Qualification = &qualification
	}
	return detail
}

func TestQueryScopeDerivesRelevanceFromGroundedCriteria(t *testing.T) {
	const query = "Антифрод и биометрия в банковских платежах"
	documents := []LiveQualificationDocument{{ID: "primary", Text: "A novel detector evaluates credit-card fraud. " +
		"The same system also authenticates biometric payment requests."}}
	for _, tc := range []struct {
		name     string
		mode     string
		criteria []string
		accept   bool
		wantErr  bool
	}{
		{name: "any_antifraud", mode: "any", criteria: []string{"b1", "c1"}, accept: true},
		{name: "any_biometrics", mode: "any", criteria: []string{"b2", "c1"}, accept: true},
		{name: "joint_missing_biometrics", mode: "all", criteria: []string{"b1", "c1"}},
		{name: "joint_both_tasks", mode: "all", criteria: []string{"b1", "b2", "c1"}, accept: true},
		{name: "foreign_industry", mode: "any", criteria: []string{"b1"}},
		{name: "only_domain", mode: "any", criteria: []string{"c1"}},
		{name: "missing_criteria", mode: "any"},
		{name: "unknown_criterion", mode: "any", criteria: []string{"b1", "c1", "other"}, wantErr: true},
		{name: "duplicate_criterion", mode: "any", criteria: []string{"b1", "c1", "b1"}, wantErr: true},
		{name: "changed_scope", mode: "any", criteria: []string{"b1", "c1"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := &LiveQueryScope{Query: strings.ToLower(query), Mode: tc.mode,
				Branches: []LiveScopeBranch{{ID: "b1", Task: "Выявление мошенничества"}, {ID: "b2", Task: "Биометрическая аутентификация"}},
				Required: []LiveScopeCondition{{ID: "c1", Condition: "Банковские платежи"}}}
			proposal := map[string]any{"label": "Проверка платёжных запросов", "technology": true, "coherent": true,
				"relevant": false, "mechanism": "Новая проверка платёжных запросов отличает мошеннические операции",
				"reason": "Описана проверка платёжных операций", "evidence": []liveQualificationSelection{
					{Source: 1, From: 1, Through: 2, Criteria: []string{"b1", "b2", "c1"}}}}
			assessment := qualificationIdentityFixture("primary", "introduces", true)
			assessment.Relevance.Criteria = tc.criteria
			assessment.Relevance.Through = 2
			review := map[string]any{"technology": true, "relevant": !tc.accept,
				"reason": "Проверены отдельные критерии запроса", "sources": []liveQualificationIdentity{assessment}}
			if tc.name == "changed_scope" {
				changed := liveCloneQueryScope(scope)
				changed.Required = nil
				review["scope"] = changed
			}
			generator := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: liveRepairJSON(t, proposal), Model: "reader"}, {Text: liveRepairJSON(t, review), Model: "reviewer"}}}
			result, err := NewCandidateQualifier(generator, func(context.Context) bool { return true })(
				liveWithQueryScope(t.Context(), scope), query, "detector", documents)
			// Отказ формы получает один повтор проверяющего с тем же scope.
			calls := 2
			if tc.wantErr {
				calls = 3
			}
			if (err != nil) != tc.wantErr || result.Verified != tc.accept || len(generator.requests) != calls {
				t.Fatalf("scope verdict: verified=%v calls=%d error=%v qualification=%+v",
					result.Verified, len(generator.requests), err, result)
			}
			if !tc.wantErr && !tc.accept {
				if code, _ := liveQualificationRejection(result); code != liveRejectOffTopic {
					t.Fatalf("unmet task/domain was accepted or misclassified: %s", code)
				}
			}
			for _, request := range generator.requests[:2] {
				var payload struct {
					Scope *LiveQueryScope `json:"scope"`
				}
				if err := json.Unmarshal([]byte(request.Query), &payload); err != nil || !liveSameQueryScope(scope, payload.Scope) {
					t.Fatalf("reader or reviewer reinterpreted the shared scope: %+v, %v", payload, err)
				}
			}
		})
	}
}
