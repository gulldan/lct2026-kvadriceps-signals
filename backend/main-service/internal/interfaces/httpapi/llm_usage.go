package httpapi

import (
	"cmp"
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jsonx"

	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func hypothesisLifecycleOps() []string { return []string{"generate", "verify", "enrich", "assess_trl"} }

type luRow struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

type luDay struct {
	Day string `json:"day"`
	luRow
}
type luModel struct {
	Model string `json:"model"`
	luRow
}
type luOp struct {
	Operation string `json:"operation"`
	luRow
}
type luPerHyp struct {
	Hypotheses  int      `json:"hypotheses"`
	Requests    float64  `json:"requests"`
	TotalTokens float64  `json:"total_tokens"`
	CostUSD     float64  `json:"cost_usd"`
	CostRub     float64  `json:"cost_rub"`
	Operations  []string `json:"operations"`
}

type luBudget struct {
	CreditsTotal     float64 `json:"credits_total"`
	CreditsUsed      float64 `json:"credits_used"`
	CreditsRemaining float64 `json:"credits_remaining"`
}

type luQuota struct {
	TodayRequests int64 `json:"today_requests"`
	DailyLimit    int   `json:"daily_limit"`
	PerMinUsed    int64 `json:"per_min_used"`
	PerMinLimit   int   `json:"per_min_limit"`
}

// luProvider — расход провайдера в его собственной валюте; между провайдерами не суммируется.
type luProvider struct {
	Key           string  `json:"key"`
	Label         string  `json:"label"`
	Currency      string  `json:"currency"`
	Kind          string  `json:"kind"`
	CostEstimated bool    `json:"cost_estimated"`
	CostRub       float64 `json:"cost_rub"`
	luRow
}

type llmUsageResponse struct {
	RangeDays     int          `json:"range_days"`
	From          string       `json:"from"`
	To            string       `json:"to"`
	Provider      string       `json:"provider"`
	Currency      string       `json:"currency"`
	CostEstimated bool         `json:"cost_estimated"`
	TotalCostRub  float64      `json:"total_cost_rub"`
	RubPerUsd     float64      `json:"rub_per_usd"`
	Totals        luRow        `json:"totals"`
	Providers     []luProvider `json:"providers"`
	ByDay         []luDay      `json:"by_day"`
	ByModel       []luModel    `json:"by_model"`
	ByOperation   []luOp       `json:"by_operation"`
	PerHypothesis luPerHyp     `json:"per_hypothesis"`
	Budget        luBudget     `json:"budget"`
	Quota         luQuota      `json:"quota"`
}

type usageAcc struct{ req, pt, ct, cost int64 }

func (a usageAcc) row() luRow {
	return luRow{
		Requests:         a.req,
		PromptTokens:     a.pt,
		CompletionTokens: a.ct,
		TotalTokens:      a.pt + a.ct,
		CostUSD:          float64(a.cost) / 1e9,
	}
}

// usageAggregate: денежные цифры на экране — только реальный расход; byProvider хранит расход
// каждого провайдера в его валюте.
type usageAggregate struct {
	totals, perHyp       usageAcc
	byDay, byModel, byOp map[string]*usageAcc
	byProvider           map[string]*usageAcc
	provMeta             map[string]providerInfo
	provEstimated        map[string]bool
	perHypByOp           map[string]int64
	allTimeRealCostNano  int64
	totalRubNano         int64
	perHypRubNano        int64
	estimated            bool
}

// aggregateUsage не смешивает валюты; строки раньше fromStr идут только в сумму бюджета.
func aggregateUsage(
	rows []*dbv1.LLMUsageDailyRow, pricing llmPricing, rules []providerRule, rubPerUsd float64, fromStr string,
) usageAggregate {
	agg := usageAggregate{
		byDay:         map[string]*usageAcc{},
		byModel:       map[string]*usageAcc{},
		byOp:          map[string]*usageAcc{},
		byProvider:    map[string]*usageAcc{},
		provMeta:      map[string]providerInfo{},
		provEstimated: map[string]bool{},
		perHypByOp:    map[string]int64{},
	}
	lifecycle := map[string]bool{}
	for _, op := range hypothesisLifecycleOps() {
		lifecycle[op] = true
	}
	add := func(m map[string]*usageAcc, k string, row *dbv1.LLMUsageDailyRow, costNano int64) {
		a := m[k]
		if a == nil {
			a = &usageAcc{}
			m[k] = a
		}
		a.req += row.GetRequests()
		a.pt += row.GetPromptTokens()
		a.ct += row.GetCompletionTokens()
		a.cost += costNano
	}
	for _, row := range rows {
		info := classifyProvider(row.GetModel(), row.GetCostNanoUsd(), rules, pricing.currency)
		var realCost int64
		if info.kind == kindReal {
			realCost = row.GetCostNanoUsd()
		}
		agg.allTimeRealCostNano += realCost
		if row.GetDay() < fromStr {
			continue
		}
		model := cmp.Or(info.model, normalizeModel(row.GetModel()))
		provCost := realCost
		if info.kind == kindNotional {
			est, estimated := estimateCostNano(row.GetPromptTokens(), row.GetCompletionTokens(), model, pricing)
			provCost = est
			if estimated {
				agg.estimated = true
				agg.provEstimated[info.label] = true
			}
		}
		rubNano := rubNanoOf(provCost, info.currency, rubPerUsd)
		agg.totalRubNano += rubNano
		if _, ok := agg.provMeta[info.label]; !ok {
			agg.provMeta[info.label] = info
		}
		add(agg.byProvider, info.label, row, provCost)
		agg.totals.req += row.GetRequests()
		agg.totals.pt += row.GetPromptTokens()
		agg.totals.ct += row.GetCompletionTokens()
		agg.totals.cost += realCost
		add(agg.byDay, row.GetDay(), row, realCost)
		add(agg.byModel, model, row, realCost)
		add(agg.byOp, row.GetOperation(), row, realCost)
		if lifecycle[row.GetOperation()] {
			agg.perHyp.req += row.GetRequests()
			agg.perHyp.pt += row.GetPromptTokens()
			agg.perHyp.ct += row.GetCompletionTokens()
			agg.perHyp.cost += realCost
			agg.perHypRubNano += rubNano
			agg.perHypByOp[row.GetOperation()] += row.GetRequests()
		}
	}
	return agg
}

func (a *API) getLLMUsage(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	ctx := r.Context()
	days := 7
	if r.URL.Query().Get("days") == "30" {
		days = 30
	}
	get := func(k string) string { return a.ovr.Get(ctx, k, "") }
	pricing := pricingFrom(get)
	rules := providersFrom(get)
	rubPerUsd := rubPerUsdFrom(get)
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -(days - 1))
	llmBase := a.ovr.Get(ctx, "VLLM_URL", "")
	model := a.ovr.Get(ctx, "VLLM_MODEL", "")
	dailyLimit, perMinLimit := quotaLimits(llmBase, model)
	// Шапка подписывается тем же классификатором, что и строки реестра, иначе
	// правило LLM_PROVIDERS переименует таблицу, а заголовок останется старым.
	self := classifyProvider(model, 0, rules, pricing.currency)
	resp := llmUsageResponse{
		RangeDays:     days,
		From:          from.Format(dayFmt),
		To:            now.Format(dayFmt),
		Provider:      self.label,
		Currency:      cmp.Or(self.currency, pricing.currency),
		Providers:     []luProvider{},
		ByDay:         []luDay{},
		ByModel:       []luModel{},
		ByOperation:   []luOp{},
		PerHypothesis: luPerHyp{Operations: hypothesisLifecycleOps()},
		Quota:         luQuota{DailyLimit: dailyLimit, PerMinLimit: perMinLimit},
	}
	if a.usage == nil {
		httpx.JSON(w, http.StatusOK, resp)
		return
	}

	// При бюджете из env читается весь реестр: «израсходовано» считается с первого дня.
	budgetTotal, _ := strconv.ParseFloat(get("LLM_BUDGET_TOTAL"), 64)
	listFrom := from
	if budgetTotal > 0 {
		listFrom = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	rows, err := a.usage.List(ctx, listFrom, now)
	if err != nil {
		a.fail(w, err)
		return
	}

	agg := aggregateUsage(rows, pricing, rules, rubPerUsd, from.Format(dayFmt))
	resp.CostEstimated = agg.estimated
	resp.Totals = agg.totals.row()
	resp.TotalCostRub = float64(agg.totalRubNano) / 1e9
	resp.RubPerUsd = rubPerUsd

	for label, acc := range agg.byProvider {
		info := agg.provMeta[label]
		resp.Providers = append(resp.Providers, luProvider{
			Key: info.key, Label: info.label, Currency: info.currency, Kind: info.kind,
			CostEstimated: agg.provEstimated[label],
			CostRub:       float64(rubNanoOf(acc.cost, info.currency, rubPerUsd)) / 1e9,
			luRow:         acc.row(),
		})
	}
	sort.Slice(resp.Providers, func(i, j int) bool {
		if ri, rj := kindRank(resp.Providers[i].Kind), kindRank(resp.Providers[j].Kind); ri != rj {
			return ri < rj
		}
		return resp.Providers[i].Requests > resp.Providers[j].Requests
	})

	for day, acc := range agg.byDay {
		resp.ByDay = append(resp.ByDay, luDay{Day: day, luRow: acc.row()})
	}
	sort.Slice(resp.ByDay, func(i, j int) bool { return resp.ByDay[i].Day < resp.ByDay[j].Day })
	for model, acc := range agg.byModel {
		resp.ByModel = append(resp.ByModel, luModel{Model: model, luRow: acc.row()})
	}
	sort.Slice(resp.ByModel, func(i, j int) bool { return resp.ByModel[i].Requests > resp.ByModel[j].Requests })
	for op, acc := range agg.byOp {
		resp.ByOperation = append(resp.ByOperation, luOp{Operation: op, luRow: acc.row()})
	}
	sort.Slice(resp.ByOperation, func(i, j int) bool { return resp.ByOperation[i].Requests > resp.ByOperation[j].Requests })

	fillPerHypothesis(&resp, agg)

	resp.Quota.TodayRequests = a.usage.TodayRequests(ctx)
	resp.Quota.PerMinUsed = a.usage.ReqPerMinute(ctx)
	if budgetTotal > 0 {
		used := float64(agg.allTimeRealCostNano) / 1e9
		resp.Budget = luBudget{CreditsTotal: budgetTotal, CreditsUsed: used, CreditsRemaining: budgetTotal - used}
	} else {
		resp.Budget = openRouterCredits(ctx, llmBase, a.ovr.Get(ctx, "VLLM_API_KEY", ""))
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// fillPerHypothesis делит расход на запросы самой нагруженной стадии: корпус в основном из
// простаивающих гипотез утянул бы цену к нулю.
func fillPerHypothesis(resp *llmUsageResponse, agg usageAggregate) {
	var processed int64 = 1
	for _, c := range agg.perHypByOp {
		if c > processed {
			processed = c
		}
	}
	resp.PerHypothesis.Hypotheses = int(processed)
	resp.PerHypothesis.Requests = float64(agg.perHyp.req) / float64(processed)
	resp.PerHypothesis.TotalTokens = float64(agg.perHyp.pt+agg.perHyp.ct) / float64(processed)
	resp.PerHypothesis.CostUSD = float64(agg.perHyp.cost) / 1e9 / float64(processed)
	resp.PerHypothesis.CostRub = float64(agg.perHypRubNano) / 1e9 / float64(processed)
}

// openRouterCredits не падает: любая ошибка даёт нули, а ручка продолжает отвечать.
func openRouterCredits(ctx context.Context, base, key string) luBudget {
	var b luBudget
	if key == "" || !strings.Contains(base, "openrouter.ai") {
		return b
	}
	url := strings.TrimRight(base, "/") + "/v1/credits"
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return b
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return b
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return b
	}
	var body struct {
		Data struct {
			TotalCredits float64 `json:"total_credits"`
			TotalUsage   float64 `json:"total_usage"`
		} `json:"data"`
	}
	if derr := jsonx.NewDecoder(resp.Body).Decode(&body); derr != nil {
		return b
	}
	b.CreditsTotal = body.Data.TotalCredits
	b.CreditsUsed = body.Data.TotalUsage
	b.CreditsRemaining = body.Data.TotalCredits - body.Data.TotalUsage
	return b
}
