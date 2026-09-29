package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

const (
	kpiSuggestMax       = 10
	kpiSuggestSampleMax = 15
	// kpiSuggestPerCluster: крупный кластер не должен заслонить остальные темы выборки.
	kpiSuggestPerCluster = 2
	kpiSuggestTopK       = 12
	kpiSuggestTimeout    = 90 * time.Second
	// kpiSuggestFallbackQuery — запрос к корпусу, пока темы не размечены: он
	// тянет выдержки о новых технологиях шести областей постановки задачи.
	kpiSuggestFallbackQuery = "новые технологии пилот внедрение коммерциализация стартап инвестиции " +
		"ИИ на устройстве защита ИИ индустриальный ИИ инфраструктура ИИ робототехника финтех"
)

// KPISuggestion ничего не сохраняет, пока пользователь не примет подсказку. Поля совпадают с вводом
// CreateKPI: фронт передаёт их как есть.
type KPISuggestion struct {
	Title        string `json:"title"`
	Metric       string `json:"metric"`
	Unit         string `json:"unit"`
	Direction    string `json:"direction"`
	FunctionArea string `json:"function_area"`
	Rationale    string `json:"rationale"`
}

type kpiSample struct {
	title   string
	topic   string
	snippet string
}

// SuggestKPIs не возвращает ошибку: без корпуса, без модели или при неразборчивом ответе страница /kpi
// получает пустой список, а не 500.
func (s *HypothesisService) SuggestKPIs(ctx context.Context, ownerID string) ([]KPISuggestion, error) {
	if s.answerer == nil {
		return nil, nil
	}
	samples, query := s.kpiCorpusSample(ctx, ownerID)

	llmCtx, cancel := withDeadline(ctx, kpiSuggestTimeout)
	defer cancel()
	resp, err := s.answerer.Answer(opCtx(llmCtx, "kpi_suggest"), &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   query,
		Prompt:  buildKPISuggestPrompt(samples),
		TopK:    kpiSuggestTopK,
	})
	if err != nil || resp == nil {
		return nil, nil
	}
	return dedupKPISuggestions(parseKPISuggestions(resp.GetAnswer())), nil
}

func (s *HypothesisService) kpiCorpusSample(ctx context.Context, ownerID string) ([]kpiSample, string) {
	clusters, err := s.cat.ListClusters(ctx, ownerID)
	if err != nil || len(clusters) == 0 {
		return nil, kpiSuggestFallbackQuery
	}
	// Выборка и запрос одинаковы от запуска к запуску; крупные темы идут первыми.
	sort.SliceStable(clusters, func(i, j int) bool {
		if clusters[i].GetDocumentCount() != clusters[j].GetDocumentCount() {
			return clusters[i].GetDocumentCount() > clusters[j].GetDocumentCount()
		}
		return clusters[i].GetLabel() < clusters[j].GetLabel()
	})

	queryTerms := make([]string, 0, len(clusters))
	for _, c := range clusters {
		if label := compactText(c.GetLabel()); label != "" {
			queryTerms = append(queryTerms, label)
		}
	}

	samples := make([]kpiSample, 0, kpiSuggestSampleMax)
	seen := map[string]struct{}{}
	// По одному представителю с кластера за проход: второй документ темы — только после всех тем.
	for pass := 0; pass < kpiSuggestPerCluster && len(samples) < kpiSuggestSampleMax; pass++ {
		for _, c := range clusters {
			if len(samples) >= kpiSuggestSampleMax {
				break
			}
			if sample, ok := kpiClusterSample(c, pass, seen); ok {
				samples = append(samples, sample)
			}
		}
	}
	query := strings.TrimSpace(strings.Join(uniqueStrings(queryTerms), " "))
	if query == "" {
		query = kpiSuggestFallbackQuery
	}
	return samples, query
}

func kpiClusterSample(c *commonv1.Cluster, pass int, seen map[string]struct{}) (kpiSample, bool) {
	reps := parseClusterRepresentatives(c.GetRepresentatives())
	if pass >= len(reps) {
		return kpiSample{}, false
	}
	rep := reps[pass]
	snippet := firstSentence(compactText(rep.Snippet), 400)
	if snippet == "" {
		return kpiSample{}, false
	}
	key := rep.DocumentID
	if key == "" {
		key = rep.Filename
	}
	if key == "" {
		key = snippet
	}
	if _, ok := seen[key]; ok {
		return kpiSample{}, false
	}
	seen[key] = struct{}{}
	return kpiSample{
		title:   kpiSampleTitle(rep.Filename, c),
		topic:   kpiClusterTopic(c),
		snippet: snippet,
	}, true
}

func kpiClusterTopic(c *commonv1.Cluster) string {
	label := compactText(c.GetLabel())
	kw := compactText(strings.Join(c.GetKeywords(), ", "))
	switch {
	case label != "" && kw != "":
		return label + " (" + kw + ")"
	case label != "":
		return label
	default:
		return kw
	}
}

func kpiSampleTitle(filename string, c *commonv1.Cluster) string {
	if t := sourceTitle(&commonv1.Source{Filename: filename}); t != "" {
		return t
	}
	if l := compactText(c.GetLabel()); l != "" {
		return l
	}
	return "документ"
}

func buildKPISuggestPrompt(samples []kpiSample) string {
	var b strings.Builder
	b.WriteString("Ты — аналитик венчурного подразделения банка. Ниже — репрезентативная выборка " +
		"работ из базы знаний, плюс выдержки из документов в контексте выше. Сформулируй " +
		"технологические и инвестиционные тезисы: какие зарождающиеся технологии из этих работ стоит " +
		"искать на радаре слабых сигналов и зачем. Области: вычисления на устройстве, защита ИИ, " +
		"индустриальный ИИ, инфраструктура ИИ, робототехника, финтех.\n\n")
	b.WriteString("Требования: (1) верни от 5 до 10 разных тезисов; (2) title — тезис одной фразой: " +
		"что ищем и зачем (напр. «Защита LLM-агентов, готовая к пилоту в корпоративной среде в 2027 " +
		"году»); (3) metric — как понять, что прогноз сбылся (напр. «технологий в стадии пилот и выше», " +
		"«пилотов у крупных заказчиков»), unit — единица, если она ясна («шт.», «%»), иначе пустая " +
		"строка, direction — increase, decrease или maintain; (4) function_area — одна из областей " +
		"выше или своя, если тезис вне их; (5) rationale — из каких работ и почему следует этот " +
		"тезис, кратко; (6) объединяй похожие тезисы и не повторяйся; (7) не выдумывай технологий, " +
		"которых нет в работах.\n\n")
	if len(samples) > 0 {
		b.WriteString("Работы:\n")
		for i, s := range samples {
			fmt.Fprintf(&b, "%d. %s", i+1, s.title)
			if s.topic != "" {
				b.WriteString(" — тема: " + s.topic)
			}
			if s.snippet != "" {
				b.WriteString(" — " + s.snippet)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString("Верни СТРОГО JSON-массив без markdown и пояснений, каждый объект по схеме:\n")
	b.WriteString(`[{"title":"тезис одной фразой","metric":"как понять, что прогноз сбылся",` +
		`"unit":"единица или пустая строка","direction":"increase|decrease|maintain",` +
		`"function_area":"область","rationale":"из каких работ и почему"}]`)
	b.WriteString(untrustedContextInstruction)
	b.WriteString(langInstruction)
	b.WriteString("\nТолько JSON-массив.")
	return b.String()
}

func parseKPISuggestions(answer string) []KPISuggestion {
	candidates := append([]string{stripJSONFence(answer)}, jsonArrayCandidates(answer)...)
	for _, raw := range candidates {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var items []KPISuggestion
		// Нужен элемент с title, а не просто непустой массив: sonic молча пропускает чужие поля, и массив-приманка
		// из <think> разобрался бы в пустые структуры и заслонил настоящий ответ дальше по тексту.
		if err := jsonx.Unmarshal([]byte(raw), &items); err == nil && hasTitledSuggestion(items) {
			return items
		}
	}
	return nil
}

func hasTitledSuggestion(items []KPISuggestion) bool {
	for _, it := range items {
		if compactText(it.Title) != "" {
			return true
		}
	}
	return false
}

func dedupKPISuggestions(items []KPISuggestion) []KPISuggestion {
	out := make([]KPISuggestion, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		it.Title = compactText(it.Title)
		it.Metric = compactText(it.Metric)
		it.Unit = compactText(it.Unit)
		it.FunctionArea = compactText(it.FunctionArea)
		it.Rationale = compactText(it.Rationale)
		it.Direction = normalizeKPIDirection(it.Direction, it.Title+" "+it.Metric)
		if it.Title == "" {
			continue
		}
		key := kpiSuggestionKey(it)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, it)
		if len(out) >= kpiSuggestMax {
			break
		}
	}
	return out
}

func kpiSuggestionKey(it KPISuggestion) string {
	base := strings.ToLower(it.Title)
	if it.Metric != "" {
		base += "|" + strings.ToLower(it.Metric)
	}
	return strings.Join(strings.Fields(base), " ")
}

const (
	kpiDirIncrease = "increase"
	kpiDirDecrease = "decrease"
	kpiDirMaintain = "maintain"
)

func normalizeKPIDirection(dir, context string) string {
	switch canon := strings.ToLower(compactText(dir)); canon {
	case kpiDirIncrease, kpiDirDecrease, kpiDirMaintain:
		return canon
	}
	hay := strings.ToLower(dir + " " + context)
	switch {
	case containsAny(hay, "decrease", "reduc", "lower", "сниз", "уменьш", "сократ", "минимиз", "удешев"):
		return kpiDirDecrease
	case containsAny(hay, "increase", "raise", "improv", "maximi", "повыс", "увелич", "улучш", "рост", "максимиз"):
		return kpiDirIncrease
	case containsAny(hay, "maintain", "stable", "поддерж", "сохран", "удерж", "стабил"):
		return kpiDirMaintain
	default:
		return kpiDirIncrease
	}
}
