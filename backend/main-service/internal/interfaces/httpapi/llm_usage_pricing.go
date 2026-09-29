package httpapi

import (
	"os"
	"strconv"
	"strings"
)

const (
	// Дефолты фри-тарифа OpenRouter; для других провайдеров лимиты задаются
	// env LLM_DAILY_LIMIT / LLM_PER_MIN_LIMIT (0 = лимита нет, UI прячет шкалу).
	freeDailyLimit  = 1000
	freePerMinLimit = 20

	dayFmt = "2006-01-02"
)

func quotaLimits(vllmURL, model string) (daily, perMin int) {
	daily, _ = strconv.Atoi(os.Getenv("LLM_DAILY_LIMIT"))
	perMin, _ = strconv.Atoi(os.Getenv("LLM_PER_MIN_LIMIT"))
	if daily == 0 && perMin == 0 &&
		strings.Contains(vllmURL, "openrouter.ai") && strings.HasSuffix(model, ":free") {
		return freeDailyLimit, freePerMinLimit
	}
	return daily, perMin
}

// normalizeModel срезает дату пермаслага OpenRouter (…-20230311:free), чтобы одна модель не дробилась на строки.
func normalizeModel(m string) string {
	base, tier := m, ""
	if i := strings.LastIndex(m, ":"); i >= 0 {
		base, tier = m[:i], m[i:]
	}
	if i := strings.LastIndex(base, "-"); i >= 0 && isDate8(base[i+1:]) {
		base = base[:i]
	}
	return base + tier
}

func isDate8(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// modelPrice — тариф модели по подстроке имени; явный 0/0 помечает известно-бесплатные
// модели, чтобы фолбэк их не переоценивал.
type modelPrice struct {
	match           string
	promptPer1M     float64
	completionPer1M float64
}

// llmPricing — прайс за 1M токенов в валюте провайдера для бэкендов, не сообщающих usage.cost.
type llmPricing struct {
	promptPer1M     float64
	completionPer1M float64
	currency        string
	models          []modelPrice
}

// pricingFrom читает LLM_PRICES («substr=in/out;…», за 1M) с фолбэком LLM_PRICE_*_PER_1M; get отдаёт
// админ-оверрайд раньше env, поэтому прайс меняется без редеплоя.
func pricingFrom(get func(string) string) llmPricing {
	p := llmPricing{currency: "₽"}
	if c := strings.TrimSpace(get("LLM_COST_CURRENCY")); c != "" {
		p.currency = c
	}
	p.promptPer1M, _ = strconv.ParseFloat(get("LLM_PRICE_PROMPT_PER_1M"), 64)
	p.completionPer1M, _ = strconv.ParseFloat(get("LLM_PRICE_COMPLETION_PER_1M"), 64)
	for _, entry := range strings.Split(get("LLM_PRICES"), ";") {
		name, rates, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		in, out, ok := strings.Cut(rates, "/")
		if !ok {
			continue
		}
		mp := modelPrice{match: strings.ToLower(strings.TrimSpace(name))}
		mp.promptPer1M, _ = strconv.ParseFloat(strings.TrimSpace(in), 64)
		mp.completionPer1M, _ = strconv.ParseFloat(strings.TrimSpace(out), 64)
		if mp.match != "" {
			p.models = append(p.models, mp)
		}
	}
	return p
}

func (p llmPricing) enabled() bool {
	return p.promptPer1M > 0 || p.completionPer1M > 0 || len(p.models) > 0
}

func (p llmPricing) rates(model string) (in, out float64) {
	m := strings.ToLower(model)
	for _, mp := range p.models {
		if strings.Contains(m, mp.match) {
			return mp.promptPer1M, mp.completionPer1M
		}
	}
	return p.promptPer1M, p.completionPer1M
}

// estimateCostNano оценивает условный расход по прайсу: цена в реестре у таких
// строк либо ноль, либо в чужой валюте (переклассифицированный OpenRouter),
// поэтому в расчёт идут только токены. Нано-единицы валюты провайдера.
func estimateCostNano(promptTokens, completionTokens int64, model string, p llmPricing) (int64, bool) {
	if !p.enabled() {
		return 0, false
	}
	in, out := p.rates(model)
	est := int64((float64(promptTokens)*in + float64(completionTokens)*out) / 1e6 * 1e9)
	return est, est > 0
}

// Вид расхода: real — живые деньги (OpenRouter, USD), notional — условные рубли Яндекса на ключе
// организатора, free — локальная модель.
const (
	kindReal     = "real"
	kindNotional = "notional"
	kindFree     = "free"

	labelYandex     = "Yandex AI Studio"
	labelOpenRouter = "OpenRouter"
	labelLocal      = "локальный LLM"
	keyOpenRouter   = "openrouter"
)

type providerInfo struct {
	key      string
	label    string
	currency string
	kind     string
	// model — имя, под которым строки правила показываются на дашборде;
	// пусто — остаётся имя из реестра.
	model string
}

type providerRule struct {
	match string
	providerInfo
}

// providersFrom читает LLM_PROVIDERS («substr=key:label:currency:kind[:model];…»). Подстрока `*` ловит всё:
// стенд с одним движком сводит историю чужих моделей в него, а пятое поле даёт этой модели одно имя.
func providersFrom(get func(string) string) []providerRule {
	var rules []providerRule
	for _, entry := range strings.Split(get("LLM_PROVIDERS"), ";") {
		name, spec, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		p := strings.SplitN(spec, ":", 5)
		if len(p) < 4 {
			continue
		}
		r := providerRule{match: strings.ToLower(strings.TrimSpace(name))}
		r.key, r.label = strings.TrimSpace(p[0]), strings.TrimSpace(p[1])
		r.currency, r.kind = strings.TrimSpace(p[2]), strings.TrimSpace(p[3])
		if len(p) == 5 {
			r.model = strings.TrimSpace(p[4])
		}
		if r.match != "" && r.kind != "" {
			rules = append(rules, r)
		}
	}
	return rules
}

func classifyProvider(model string, costNano int64, rules []providerRule, envCurrency string) providerInfo {
	m := strings.ToLower(model)
	for _, r := range rules {
		if r.match == "*" || strings.Contains(m, r.match) {
			return r.providerInfo
		}
	}
	switch {
	case strings.Contains(m, "yandexgpt") || strings.Contains(m, "gpt://"):
		return providerInfo{key: "yandex", label: labelYandex, currency: envCurrency, kind: kindNotional}
	case costNano > 0 || strings.Contains(m, "/"):
		// Слаг без цены (:free или неоценённый вызов) — real $0, а не free: иначе корзина OpenRouter
		// раскололась бы на ложный бесплатный вид.
		return providerInfo{key: keyOpenRouter, label: labelOpenRouter, currency: "$", kind: kindReal}
	default:
		return providerInfo{key: "local", label: labelLocal, currency: "", kind: kindFree}
	}
}

func kindRank(kind string) int {
	switch kind {
	case kindReal:
		return 0
	case kindNotional:
		return 1
	default:
		return 2
	}
}

func rubPerUsdFrom(get func(string) string) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(get("LLM_RUB_PER_USD")), 64); err == nil && v > 0 {
		return v
	}
	return 90
}

func rubNanoOf(costNano int64, currency string, rubPerUsd float64) int64 {
	if currency == "$" {
		return int64(float64(costNano) * rubPerUsd)
	}
	return costNano
}
