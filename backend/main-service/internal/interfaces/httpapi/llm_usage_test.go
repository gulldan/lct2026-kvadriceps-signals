package httpapi

import (
	"os"
	"strings"
	"testing"

	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func TestEstimateCostNano(t *testing.T) {
	price := llmPricing{promptPer1M: 0.6, completionPer1M: 2.4, currency: "₽"}
	tests := []struct {
		name         string
		prompt, comp int64
		p            llmPricing
		want         int64
		wantEst      bool
	}{
		// 1M входных + 0,5M выходных по 0,6/2,4 за 1M = 1,8 → 1,8e9 нано.
		{"estimates by price list", 1_000_000, 500_000, price, 1_800_000_000, true},
		{"no price list", 1_000_000, 500_000, llmPricing{currency: "$"}, 0, false},
		{"no tokens", 0, 0, price, 0, false},
	}
	for _, tt := range tests {
		got, est := estimateCostNano(tt.prompt, tt.comp, "any-model", tt.p)
		if got != tt.want || est != tt.wantEst {
			t.Errorf("%s: estimateCostNano = (%d, %v), want (%d, %v)", tt.name, got, est, tt.want, tt.wantEst)
		}
	}
}

func TestEstimateCostNano_PerModel(t *testing.T) {
	p := llmPricing{
		promptPer1M: 9, completionPer1M: 9, currency: "₽",
		models: []modelPrice{
			{match: "deepseek-v4-flash", promptPer1M: 300, completionPer1M: 500},
			{match: ":free", promptPer1M: 0, completionPer1M: 0},
		},
	}
	got, est := estimateCostNano(1_000_000, 1_000_000, "gpt://f/deepseek-v4-flash/latest", p)
	if got != 800_000_000_000 || !est {
		t.Fatalf("deepseek = (%d, %v), want (8e11, true)", got, est)
	}
	if got, est = estimateCostNano(1_000_000, 0, "nvidia/nemotron:free", p); got != 0 || est {
		t.Fatalf("known-free = (%d, %v), want (0, false)", got, est)
	}
	if got, _ = estimateCostNano(1_000_000, 0, "other-model", p); got != 9_000_000_000 {
		t.Fatalf("fallback = %d, want 9e9", got)
	}
}

func TestAggregateUsage_BudgetTail(t *testing.T) {
	price := llmPricing{promptPer1M: 1, completionPer1M: 1}
	op := hypothesisLifecycleOps()[0]
	rows := []*dbv1.LLMUsageDailyRow{
		{Day: "2025-01-05", Model: "deepseek/x", Operation: op, Requests: 2, CostNanoUsd: 1_000_000_000},
		{Day: "2026-07-01", Model: "deepseek/x", Operation: op, Requests: 3, CostNanoUsd: 2_000_000_000},
	}
	agg := aggregateUsage(rows, price, nil, 90, "2026-06-01")
	if got := agg.allTimeRealCostNano; got != 3_000_000_000 { // $1 + $2 реальных
		t.Fatalf("allTimeRealCostNano = %d, want 3e9", got)
	}
	if agg.totals.req != 3 || agg.totals.cost != 2_000_000_000 {
		t.Fatalf("range totals = %+v, want only the in-range row", agg.totals)
	}
	if got := agg.totalRubNano; got != 180_000_000_000 { // $2 × 90 ₽/$
		t.Fatalf("totalRubNano = %d, want 180e9", got)
	}
	if len(agg.byDay) != 1 || agg.perHyp.req != 3 {
		t.Fatalf("byDay/perHyp include the budget-only tail: %v, %+v", agg.byDay, agg.perHyp)
	}
}

func TestClassifyProvider(t *testing.T) {
	cases := []struct {
		model             string
		cost              int64
		wantKey, wantKind string
	}{
		{"gpt://folder/yandexgpt/latest", 0, "yandex", kindNotional},
		{"deepseek/deepseek-chat-v4", 12_345, "openrouter", kindReal},
		{"nvidia/nemotron:free", 0, "openrouter", kindReal},
		{"qwen3-35b-a3b", 0, "local", kindFree},
	}
	for _, c := range cases {
		got := classifyProvider(c.model, c.cost, nil, "₽")
		if got.key != c.wantKey || got.kind != c.wantKind {
			t.Errorf("classifyProvider(%q, %d) = %s/%s, want %s/%s",
				c.model, c.cost, got.key, got.kind, c.wantKey, c.wantKind)
		}
	}
}

func TestAggregateUsage_ProviderSplit(t *testing.T) {
	price := llmPricing{currency: "₽", models: []modelPrice{{match: "yandexgpt", promptPer1M: 800, completionPer1M: 800}}}
	op := hypothesisLifecycleOps()[0]
	day := "2026-07-01"
	rows := []*dbv1.LLMUsageDailyRow{
		{Day: day, Model: "deepseek/x", Operation: op, Requests: 1, CostNanoUsd: 2_000_000_000},
		{Day: day, Model: "gpt://f/yandexgpt/latest", Operation: op, Requests: 1, PromptTokens: 1_000_000},
		{Day: day, Model: "qwen3-35b-a3b", Operation: op, Requests: 1, PromptTokens: 500_000},
	}
	agg := aggregateUsage(rows, price, nil, 90, "2026-06-01")
	if len(agg.byProvider) != 3 {
		t.Fatalf("byProvider = %d buckets, want 3", len(agg.byProvider))
	}
	if or := agg.byProvider["OpenRouter"]; or == nil || or.cost != 2_000_000_000 {
		t.Fatalf("OpenRouter bucket = %+v, want cost 2e9 ($)", or)
	}
	if ya := agg.byProvider["Yandex AI Studio"]; ya == nil || ya.cost != 800_000_000_000 { // 1M×800/1M = 800 ₽
		t.Fatalf("Yandex bucket = %+v, want cost 800e9 (₽)", ya)
	}
	// Общий итог в ₽: $2×90 + 800 ₽ = 980 ₽; локальная модель ничего не добавляет.
	if agg.totalRubNano != 980_000_000_000 {
		t.Fatalf("totalRubNano = %d, want 980e9", agg.totalRubNano)
	}
}

func TestPricingFrom(t *testing.T) {
	for _, k := range []string{"LLM_PRICE_PROMPT_PER_1M", "LLM_PRICE_COMPLETION_PER_1M", "LLM_COST_CURRENCY", "LLM_PRICES"} {
		t.Setenv(k, "")
	}
	if p := pricingFrom(os.Getenv); p.enabled() || p.currency != "₽" {
		t.Fatalf("empty env: pricing = %+v, want disabled with ₽", p)
	}
	t.Setenv("LLM_PRICE_PROMPT_PER_1M", "0.6")
	t.Setenv("LLM_PRICE_COMPLETION_PER_1M", "2.4")
	t.Setenv("LLM_COST_CURRENCY", "$")
	p := pricingFrom(os.Getenv)
	if !p.enabled() || p.promptPer1M != 0.6 || p.completionPer1M != 2.4 || p.currency != "$" {
		t.Fatalf("pricing = %+v", p)
	}
}

// Правило `*` с пятым полем сводит историю всех провайдеров в один условный
// движок под одним именем модели; реальные доллары OpenRouter при этом не
// протекают в рубли — строка переоценивается по прайсу этой модели.
func TestAggregateUsage_CatchAllRule(t *testing.T) {
	rules := providersFrom(func(string) string {
		return "*=local:Локальный ИИ:₽:notional:local/deepseek-v4-flash-abliterated"
	})
	price := llmPricing{currency: "₽", models: []modelPrice{{match: "deepseek-v4-flash", promptPer1M: 300, completionPer1M: 500}}}
	op := hypothesisLifecycleOps()[0]
	day := "2026-09-17"
	rows := []*dbv1.LLMUsageDailyRow{
		{Day: day, Model: "deepseek/x", Operation: op, Requests: 1, PromptTokens: 1_000_000, CostNanoUsd: 2_000_000_000},
		{Day: day, Model: "gpt://f/yandexgpt/latest", Operation: op, Requests: 1, CompletionTokens: 1_000_000},
		{Day: day, Model: "corp/deepseek-v4-flash-abliterated", Operation: op, Requests: 1, PromptTokens: 1_000_000},
	}
	agg := aggregateUsage(rows, price, rules, 90, "2026-06-01")
	if len(agg.byProvider) != 1 || len(agg.byModel) != 1 {
		t.Fatalf("buckets = %d providers / %d models, want 1 / 1", len(agg.byProvider), len(agg.byModel))
	}
	if _, ok := agg.byModel["local/deepseek-v4-flash-abliterated"]; !ok {
		t.Fatalf("byModel keys = %v, want the rule's model name", agg.byModel)
	}
	// 2M входных × 300 + 1M выходных × 500 = 1100 ₽; $2 OpenRouter не учитываются.
	if agg.totalRubNano != 1_100_000_000_000 || agg.totals.cost != 0 {
		t.Fatalf("totalRubNano = %d, real cost = %d; want 1100e9 / 0", agg.totalRubNano, agg.totals.cost)
	}
}

func TestProjectJSONBranches(t *testing.T) {
	src := `{"ranking":{"score":0.7,"factors":[{"key":"x","detail":"длинный текст"}]},` +
		`"itc":{"score":6,"components":{"SM":{"note":"тяжёлый"}}},"check":{"verdict":"supported","rationale":"много букв"}}`
	got := string(projectJSONBranches(src, boardAssessmentPaths()...))
	for _, want := range []string{`"score":0.7`, `"verdict":"supported"`, `"itc"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("projection lost %s: %s", want, got)
		}
	}
	for _, banned := range []string{"factors", "components", "rationale"} {
		if strings.Contains(got, banned) {
			t.Fatalf("projection leaked %s: %s", banned, got)
		}
	}
	if string(projectJSONBranches("not-json", boardAssessmentPaths()...)) != "null" {
		t.Fatal("invalid JSON must fall back to rawJSON null")
	}
}
