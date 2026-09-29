package application

import (
	"context"
	"strings"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

const (
	kpiParseTimeout = 45 * time.Second
	// kpiParseTopK мал: немного опоры на корпус без роста задержки.
	kpiParseTopK = 4
)

// KPIParseResult: поля совпадают с kpiInput, фронт передаёт результат в POST /kpis как есть.
type KPIParseResult struct {
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Metric       string   `json:"metric"`
	Unit         string   `json:"unit"`
	Direction    string   `json:"direction"`
	FunctionArea string   `json:"function_area"`
	Constraints  string   `json:"constraints"`
	Baseline     *float64 `json:"baseline"`
	Target       *float64 `json:"target"`
}

// ParseKPIPrompt не падает: без модели или при неразборчивом ответе первая строка становится названием,
// а весь текст — описанием. Одно поле вместо формы — просьба экспертов.
func (s *HypothesisService) ParseKPIPrompt(ctx context.Context, ownerID, prompt string) KPIParseResult {
	fallback := kpiParseFallback(prompt)
	if s.answerer == nil || strings.TrimSpace(prompt) == "" {
		return fallback
	}
	llmCtx, cancel := withDeadline(ctx, kpiParseTimeout)
	defer cancel()
	resp, err := s.answerer.Answer(opCtx(llmCtx, "kpi_parse"), &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   firstSentence(compactText(prompt), 200),
		Prompt:  buildKPIParsePrompt(prompt),
		TopK:    kpiParseTopK,
	})
	if err != nil || resp == nil {
		return fallback
	}
	parsed, ok := parseKPIParseAnswer(resp.GetAnswer())
	if !ok {
		return fallback
	}
	if parsed.Title == "" {
		parsed.Title = fallback.Title
	}
	if parsed.Description == "" {
		parsed.Description = prompt
	}
	parsed.Direction = normalizeKPIDirection(parsed.Direction, parsed.Title+" "+parsed.Metric+" "+prompt)
	return parsed
}

func buildKPIParsePrompt(prompt string) string {
	var b strings.Builder
	b.WriteString("Ты помогаешь аналитику сформулировать технологический или инвестиционный тезис: ")
	b.WriteString("что искать на радаре слабых сигналов и зачем. Разбери его формулировку на структурные поля.\n\n")
	b.WriteString("Формулировка аналитика:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n\"\"\"\n\n")
	b.WriteString("Верни СТРОГО один JSON-объект без пояснений:\n")
	b.WriteString(`{"title": "короткое название тезиса (до 90 символов, по-русски)",` + "\n")
	b.WriteString(` "description": "суть тезиса своими словами, 1-3 предложения",` + "\n")
	b.WriteString(` "metric": "измеряемая величина, например: технологий в стадии пилот и выше",` + "\n")
	b.WriteString(` "unit": "единица измерения, например: шт.",` + "\n")
	b.WriteString(` "direction": "increase | decrease | maintain",` + "\n")
	b.WriteString(` "function_area": "область, например: защита ИИ, робототехника, финтех",` + "\n")
	b.WriteString(` "baseline": число — текущее значение показателя, или null если не названо,` + "\n")
	b.WriteString(` "target": число — целевое значение показателя, или null если не названо,` + "\n")
	b.WriteString(` "constraints": "все ограничения из формулировки (бюджет, горизонт, юрисдикция, лицензии)` +
		` одной строкой; пустая строка если их нет"}` + "\n\n")
	b.WriteString("Не выдумывай ограничения и числа, которых нет в формулировке.")
	return b.String()
}

func parseKPIParseAnswer(answer string) (KPIParseResult, bool) {
	var out KPIParseResult
	raw := stripJSONFence(answer)
	if start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	if err := jsonx.Unmarshal([]byte(raw), &out); err != nil {
		return KPIParseResult{}, false
	}
	out.Title = firstSentence(compactText(out.Title), 120)
	out.Metric = compactText(out.Metric)
	out.Unit = compactText(out.Unit)
	out.FunctionArea = compactText(out.FunctionArea)
	out.Constraints = strings.TrimSpace(out.Constraints)
	out.Description = strings.TrimSpace(out.Description)
	return out, out.Title != "" || out.Metric != "" || out.Description != ""
}

func kpiParseFallback(prompt string) KPIParseResult {
	trimmed := strings.TrimSpace(prompt)
	title := trimmed
	if i := strings.IndexByte(trimmed, '\n'); i > 0 {
		title = trimmed[:i]
	}
	title = firstSentence(compactText(title), 120)
	if title == "" {
		title = "Новая цель"
	}
	return KPIParseResult{
		Title:       title,
		Description: trimmed,
		Direction:   normalizeKPIDirection("", trimmed),
	}
}
