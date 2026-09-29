package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Один разбор запроса переживает все волны и проверки: повторная интерпретация меняла перечисление на сочетание.
const liveSearchTermsSystem = `Build one compact retrieval and acceptance plan for the user's technology direction.
Return JSON {"terms":["technical phrase"],"scope":{"mode":"any","branches":[{"id":"b1","task":"user task"}],
"required":[{"id":"c1","condition":"shared constraint"}]}}. No prose outside JSON.
terms: up to 12 distinct, established English technical phrases, normally 2-3 words. Each phrase is searched
VERBATIM in titles/abstracts; phrases are combined with OR. Cover complementary mechanisms, not synonyms alone.
Never concatenate domain+method+purpose into invented phrases. Banking fraud and biometrics can retrieve
"fraud detection" and "biometric authentication", NOT "anti-fraud biometric banking".
scope: interpret the user's intent ONCE, without adding retrieval subareas or synonyms as acceptance criteria.
Use 1-4 independent task branches with sequential IDs b1,b2,...; a single task uses one branch.
mode=any for an enumeration of independent directions, including "antifraud and biometrics in banking payments":
either fraud detection OR biometric authentication is sufficient. mode=all ONLY when the user explicitly requires
the same technology to combine the tasks ("jointly", "simultaneously", "combining", "using X for Y").
Keep all stated application, material and operating constraints in 0-3 shared required conditions c1,c2,... .
Each task/condition must be concise, at most 160 characters, and grounded in the original request, not invented.
For banking payments, credit-card/payment fraud or authentication research can fit without the literal word banking;
an unrelated industry-only application is not evidence of this domain merely because future transfer is imaginable.
Do not require a commercial deployment when the request permits emerging research.
Keep attack detection distinct from creating attacks. Do not broaden solid-electrolyte batteries to unrelated energy devices.
Drop framing such as "weak signals", "emerging technologies", "слабые сигналы"; retain the actual tasks and constraints.
Every downstream stage will use this scope unchanged; terms are retrieval aids, never evidence of relevance.`

func NewLiveQueryPlanner(gen aiclients.Generator, configured func(context.Context) bool) LiveQueryPlanner {
	return func(ctx context.Context, query string) (LiveQueryPlan, error) {
		if configured == nil || !configured(ctx) {
			return LiveQueryPlan{}, errLLMNotConfigured
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out, err := gen.Generate(ctx, aiclients.GenRequest{
			System: liveSearchTermsSystem, Query: query, JSONMode: true, MaxTokens: 640, Temperature: 0,
		})
		if err != nil {
			return LiveQueryPlan{}, fmt.Errorf("live query plan: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return LiveQueryPlan{}, fmt.Errorf("live query plan: %w", err)
		}
		if out.FinishReason != "" && out.FinishReason != "stop" {
			return LiveQueryPlan{}, fmt.Errorf("live query plan: incomplete response (%s)", out.FinishReason)
		}
		var plan LiveQueryPlan
		if err := json.Unmarshal([]byte(liveJSONObject(out.Text)), &plan); err != nil {
			return LiveQueryPlan{}, fmt.Errorf("live query plan: invalid response: %w", err)
		}
		if plan.Scope != nil {
			plan.Scope.Query, _ = normalizeLiveQuery(query)
		}
		if err := liveValidateQueryPlan(plan, query); err != nil {
			return LiveQueryPlan{}, err
		}
		plan.Terms = liveDropCoveredWords(plan.Terms)
		return plan, nil
	}
}

func liveValidateQueryPlan(plan LiveQueryPlan, query string) error {
	if err := liveValidateQueryScope(plan.Scope, query); err != nil {
		return fmt.Errorf("live query plan: %w", err)
	}
	if len(plan.Terms) == 0 || len(plan.Terms) > LiveSearchTerms {
		return errors.New("live query plan: expected 1-12 search phrases")
	}
	for _, term := range plan.Terms {
		if !liveScopeText(term) || strings.ContainsAny(term, `"\`) {
			return errors.New("live query plan: invalid search phrase")
		}
	}
	return nil
}

func (s *TrendLiveService) planQuery(ctx context.Context, query string) (LiveQueryPlan, error) {
	if s.plan == nil {
		return LiveQueryPlan{}, errors.New("live query plan: planner unavailable")
	}
	plan, err := s.plan(ctx, query)
	if err != nil {
		return LiveQueryPlan{}, err
	}
	if err := liveValidateQueryPlan(plan, query); err != nil {
		return LiveQueryPlan{}, err
	}
	plan.Terms = liveDropCoveredWords(plan.Terms)
	return plan, nil
}
