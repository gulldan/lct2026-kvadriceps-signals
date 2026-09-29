package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	liveScopeAny = "any"
	liveScopeAll = "all"
)

// LiveQueryPlan разделяет поиск и критерии допуска, чтобы синоним не расширял задачу пользователя.
type LiveQueryPlan struct {
	Terms []string        `json:"terms"`
	Scope *LiveQueryScope `json:"scope"`
}

type LiveQueryScope struct {
	Query    string               `json:"query"`
	Mode     string               `json:"mode"`
	Branches []LiveScopeBranch    `json:"branches"`
	Required []LiveScopeCondition `json:"required"`
}

type LiveScopeBranch struct {
	ID   string `json:"id"`
	Task string `json:"task"`
}

type LiveScopeCondition struct {
	ID        string `json:"id"`
	Condition string `json:"condition"`
}

type LiveQueryPlanner func(context.Context, string) (LiveQueryPlan, error)

type liveQueryScopeKey struct{}

func liveWithQueryScope(ctx context.Context, scope *LiveQueryScope) context.Context {
	return context.WithValue(ctx, liveQueryScopeKey{}, liveCloneQueryScope(scope))
}

func liveCloneQueryScope(scope *LiveQueryScope) *LiveQueryScope {
	if scope == nil {
		return nil
	}
	cloned := *scope
	cloned.Branches, cloned.Required = slices.Clone(scope.Branches), slices.Clone(scope.Required)
	return &cloned
}

func liveQueryScope(ctx context.Context, query string) (*LiveQueryScope, error) {
	scope, _ := ctx.Value(liveQueryScopeKey{}).(*LiveQueryScope)
	if err := liveValidateQueryScope(scope, query); err != nil {
		return nil, err
	}
	return liveCloneQueryScope(scope), nil
}

func liveValidateQueryScope(scope *LiveQueryScope, query string) error {
	if scope == nil {
		return errors.New("query scope: missing structured plan")
	}
	normalized, err := normalizeLiveQuery(query)
	if err != nil || scope.Query != normalized {
		return errors.New("query scope: plan belongs to a different query")
	}
	if scope.Mode != liveScopeAny && scope.Mode != liveScopeAll {
		return errors.New("query scope: mode must be any or all")
	}
	if len(scope.Branches) == 0 || len(scope.Branches) > 4 || scope.Required == nil || len(scope.Required) > 3 {
		return errors.New("query scope: expected 1-4 branches and at most 3 shared conditions")
	}
	for i, branch := range scope.Branches {
		if branch.ID != fmt.Sprintf("b%d", i+1) || !liveScopeText(branch.Task) {
			return errors.New("query scope: invalid branch ID or task")
		}
	}
	for i, condition := range scope.Required {
		if condition.ID != fmt.Sprintf("c%d", i+1) || !liveScopeText(condition.Condition) {
			return errors.New("query scope: invalid condition ID or constraint")
		}
	}
	return nil
}

func liveScopeText(text string) bool {
	return strings.TrimSpace(text) == text && utf8.RuneCountInString(text) >= 2 &&
		utf8.RuneCountInString(text) <= 160 && !strings.ContainsFunc(text, unicode.IsControl)
}

func liveValidateScopeCriteria(scope *LiveQueryScope, criteria []string) error {
	known := make(map[string]bool, len(scope.Branches)+len(scope.Required))
	for _, branch := range scope.Branches {
		known[branch.ID] = true
	}
	for _, condition := range scope.Required {
		known[condition.ID] = true
	}
	for _, id := range criteria {
		if !known[id] {
			return fmt.Errorf("query scope: unknown or duplicate criterion %q", id)
		}
		known[id] = false
	}
	return nil
}

func liveScopeSatisfied(scope *LiveQueryScope, criteria []string) bool {
	matched := 0
	for _, branch := range scope.Branches {
		if slices.Contains(criteria, branch.ID) {
			matched++
		}
	}
	if matched == 0 || (scope.Mode == liveScopeAll && matched != len(scope.Branches)) {
		return false
	}
	return !slices.ContainsFunc(scope.Required, func(condition LiveScopeCondition) bool {
		return !slices.Contains(criteria, condition.ID)
	})
}

func liveSameQueryScope(a, b *LiveQueryScope) bool {
	return a != nil && b != nil && a.Query == b.Query && a.Mode == b.Mode &&
		slices.Equal(a.Branches, b.Branches) && slices.Equal(a.Required, b.Required)
}

const liveQueryScopeInstruction = `
Поле scope — неизменный разбор задачи пользователя, а не новое предложение модели.
Используй только его ветви и обязательные условия; не интерпретируй связки исходного query заново.
mode=any допускает одну ветвь, mode=all требует все ветви в одном механизме; все required обязательны.
Наличие scope не доказывает факты или применение: они по-прежнему требуют прямых исходных свидетельств.
Поисковые синонимы не входят в критерии допуска и не расширяют область применения.`

// Граница хранилища не доверяет только версии формата или общему положительному флагу.
func liveValidAnswerScope(scope *LiveQueryScope, query string, candidates []LiveTopic) bool {
	if liveValidateQueryScope(scope, query) != nil {
		return false
	}
	for _, candidate := range candidates {
		qualification := candidate.Qualification
		// Добранная проверку не прошла и помечена, но проверку другой области ответ не принимает.
		if candidate.Shortfall != nil {
			if qualification != nil && !liveSameQueryScope(scope, qualification.Scope) {
				return false
			}
			continue
		}
		if !liveVerifiedMechanism(candidate) || qualification.Method != liveQualificationMethod ||
			!liveSameQueryScope(scope, qualification.Scope) || liveValidateQualificationScope(*qualification) != nil {
			return false
		}
	}
	return true
}
