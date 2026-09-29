// Второй этап генерации: паспорт гипотезы дополняется по полному тексту источников одним вызовом LLM.
// Слияние не затирает поля генерации и планировщика эксперимента; фильтр качества архивирует пустые и дубли.

package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"github.com/example/main-service/internal/platform/jsonx"
	"github.com/example/main-service/internal/platform/logger"
	"github.com/example/main-service/internal/platform/runtimecfg"
)

const (
	// enrichEvidenceTop мал: основной контекст — полный текст источников в промпте, поиск лишь добавляет опору.
	enrichEvidenceTop      = 6
	enrichMaxRunes         = 12000
	genQualityFloorDefault = 0.35
	actionEnriched         = "enriched"
	statusArchived         = "archived"
	keyScore               = "score"
	keyMissing             = "missing"
)

var ErrInsufficientContext = errors.New("insufficient source context for hypothesis enrichment")

// genBreadthEnabled: лёгкая схема при любом числе гипотез — больше идей, богатые поля откладываются на второй этап.
func genBreadthEnabled(ctx context.Context, ovr *runtimecfg.Overrides) bool {
	return ovr.GetBool(ctx, "RAG_GEN_BREADTH", true)
}

func useLightGenSchema(ctx context.Context, ovr *runtimecfg.Overrides, count int) bool {
	return genBreadthEnabled(ctx, ovr) || compactGenPrompt(count)
}

func genQualityFloor(ctx context.Context, ovr *runtimecfg.Overrides) float64 {
	return ovr.GetFloat(ctx, "RAG_GEN_QUALITY_FLOOR", genQualityFloorDefault)
}

// enrichResult повторяет формы полей genItem, чтобы переиспользовать очистители генерации.
type enrichResult struct {
	CausalChain             []causalStep   `json:"causal_chain"`
	Experiment              *experimentVal `json:"experiment_plan"`
	Feasibility             []feasibility  `json:"feasibility"`
	MicrostructureMechanism enrichText     `json:"microstructure_mechanism"`
	CompositionChange       enrichText     `json:"composition_change"`
	ProcessChange           enrichText     `json:"process_change"`
	CharacterizationMethods []string       `json:"characterization_methods"`
	TestMethods             []string       `json:"test_methods"`
	FailureModes            []string       `json:"failure_modes"`
}

func (s *HypothesisService) EnrichHypothesis(ctx context.Context, ownerID, id string) (*commonv1.Hypothesis, error) {
	// Полный текст и подробный паспорт требуют того же бюджета, что и генерация гипотез.
	settings := s.runtimeSettings(ctx, ownerID)
	ctx, cancel := withDeadline(ctx, time.Duration(settings.GenerationTimeoutSec)*time.Second)
	defer cancel()
	h, err := s.GetHypothesis(ctx, ownerID, false, id)
	if err != nil {
		return nil, err
	}
	docText := s.sourceDocumentsText(ctx, ownerID, h)
	resp, aerr := s.answerer.Answer(opCtx(ctx, "enrich"), &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   h.GetStatement(),              // короткая — для дополнительного поиска
		Prompt:  buildEnrichPrompt(h, docText), // полный текст источников — только для LLM
		TopK:    enrichEvidenceTop,
	})
	if aerr != nil {
		return nil, aerr
	}
	if resp.GetTrace().GetAbstainReason() != "" {
		return nil, ErrInsufficientContext
	}
	res, perr := parseEnrich(resp.GetAnswer())
	if perr != nil {
		return nil, perr
	}
	model := resp.GetModel()
	h.Detail = mergeEnrichDetail(h.GetDetail(), res, model)

	score, missing, hasMechanism, hasPlan := detailSignals(h.GetDetail(), h.GetMeasurable())
	h.Assessment = mergeEnrichAssessment(h.GetAssessment(), score, missing, model)

	grounded := hasGroundedEvidence(h)
	duplicate := s.isDuplicateStatement(ctx, ownerID, id, h.GetStatement())
	reason, archived := qualityGate(score, hasMechanism, hasPlan, grounded, duplicate, genQualityFloor(ctx, s.ovr))
	// Фильтр отсеивает только машинный мусор и не отменяет решение человека: одобренная гипотеза сохраняет статус.
	if archived && h.GetStatus() == statusApproved {
		archived, reason = false, ""
	}
	summary := "Обогащение по полному тексту источников"
	if archived {
		h.Status = statusArchived
		summary = "Отсев гипотезы: " + archiveReasonRU(reason)
		logger.From(ctx).Info().
			Str("hypothesis_id", id).Str("reason", reason).
			Msg("hypothesis archived by quality gate")
	}
	// Паспорт и статус изменились — пересчитать рейтинг, иначе доска сортирует по старым признакам.
	s.applyRanking(ctx, h)
	rev := &commonv1.HypothesisRevision{HypothesisId: id, Action: actionEnriched, EditorId: editorSystem, Summary: summary}
	if uerr := s.cat.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		return nil, uerr
	}
	return s.GetHypothesis(ctx, ownerID, false, id)
}

// sourceDocumentsText при ошибках чтения отдаёт уже собранное: обогащение деградирует до контекста поиска, а не падает.
func (s *HypothesisService) sourceDocumentsText(ctx context.Context, ownerID string, h *commonv1.Hypothesis) string {
	if s.chunks == nil {
		return ""
	}
	seen := make(map[string]struct{})
	var b strings.Builder
	runes := 0
	for _, e := range h.GetEvidence() {
		docID := strings.TrimSpace(e.GetDocumentId())
		if docID == "" {
			continue
		}
		if _, ok := seen[docID]; ok {
			continue
		}
		seen[docID] = struct{}{}
		chunks, err := s.chunks.DocumentChunks(ctx, ownerID, docID)
		if err != nil {
			continue
		}
		for _, c := range chunks {
			text := strings.TrimSpace(c.GetText())
			if text == "" {
				continue
			}
			b.WriteString(text)
			b.WriteByte('\n')
			runes += len([]rune(text)) + 1
			if runes >= enrichMaxRunes {
				return capRunes(b.String(), enrichMaxRunes)
			}
		}
	}
	return capRunes(b.String(), enrichMaxRunes)
}

func capRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// buildEnrichPrompt просит только богатые поля паспорта: заголовок и оценки принадлежат первому этапу.
func buildEnrichPrompt(h *commonv1.Hypothesis, docText string) string {
	var b strings.Builder
	b.WriteString("Гипотеза: «")
	b.WriteString(h.GetStatement())
	b.WriteString("».")
	if r := h.GetRationale(); r != "" {
		b.WriteString(" Обоснование: ")
		b.WriteString(r)
	}
	if d := compactText(h.GetDetail()); d != "" {
		b.WriteString("\nТекущий паспорт гипотезы (JSON): ")
		b.WriteString(d)
	}
	if docText != "" {
		b.WriteString("\n\nПолный текст документов-источников:\n")
		b.WriteString(docText)
	}
	b.WriteString("\n\nТы — ассистент НИОКР. Опираясь на гипотезу и приведённый выше полный текст источников, " +
		"дополни паспорт гипотезы недостающими деталями: причинно-следственная цепочка (для материаловедения: " +
		"состав → процесс → микроструктура → свойство → KPI; для обогащения руды и металлургии: " +
		"питание/сырьё → операция/аппарат → режим → технологический показатель → KPI); " +
		"план эксперимента (материалы, параметры процесса " +
		"с диапазонами, методы характеризации и испытаний, контроли, ИЗМЕРИМЫЙ критерий успеха, ориентировочные " +
		"стоимость и срок); реализуемость по аспектам (научная реализуемость, масштабирование, безопасность/экология, " +
		"цепочка поставок) с уровнем low/medium/high; микроструктурный механизм; изменения состава и процесса; методы " +
		"характеризации и испытаний; режимы отказа. Заполняй ТОЛЬКО на основе источников; если данных нет — оставь поле " +
		"пустым согласно типу в схеме: строковые поля — \"\", списки — [], объект плана — {}. " +
		"microstructure_mechanism, composition_change и process_change — строки, для них используй \"\", а не []. " +
		"Не выдумывай сведения.")
	b.WriteString("\nВерни СТРОГО JSON без markdown по схеме:\n")
	b.WriteString(`{"causal_chain":[{"stage":"состав|процесс|микроструктура|свойство|питание/сырьё|операция|режим|показатель|KPI",` +
		`"change":"что меняется"}],` +
		`"experiment_plan":{"experiment_type":"new_alloy|process_route|coating_corrosion|battery_material|` +
		`ore_beneficiation|metallurgy_process|generic",` +
		`"sections":[{"title":"этап","purpose":"зачем","items":["конкретное действие"]}],` +
		`"materials":["материал/реактив"],"process_parameters":[{"name":"параметр","range":"диапазон с единицами"}],` +
		`"characterization_methods":["SEM","XRD"],"test_methods":["метод испытания"],"controls":["базовый образец"],` +
		`"success_criteria":"измеримый критерий успеха","estimated_cost":"low|medium|high","estimated_time":"days|weeks|months",` +
		`"risks":["ключевой риск"],"variables":["управляемая переменная"],"methods":["метод"],"horizon":"срок"},` +
		`"feasibility":[{"aspect":"научная реализуемость|масштабирование|безопасность/экология|цепочка поставок",` +
		`"level":"low|medium|high","note":"кратко почему"}],` +
		`"microstructure_mechanism":"ожидаемый механизм","composition_change":"что меняем в составе",` +
		`"process_change":"какой техрежим/процесс меняем","characterization_methods":["SEM","TEM"],` +
		`"test_methods":["tensile test"],"failure_modes":["что может пойти не так"]}`)
	b.WriteString(untrustedContextInstruction)
	b.WriteString(langInstruction)
	b.WriteString("\nТолько JSON-объект.")
	return b.String()
}

func parseEnrich(answer string) (enrichResult, error) {
	var res enrichResult
	if err := decodeJSONObject(answer, &res, jsonx.Unmarshal); err != nil {
		if errors.Is(err, errNoJSONObject) {
			return enrichResult{}, errors.New("no JSON object in enrich output")
		}
		return enrichResult{}, fmt.Errorf("parse enrich result: %w", err)
	}
	return res, nil
}

// mergeEnrichDetail заполняет поле, только если оно пусто: значения генерации и планировщика не затираются.
func mergeEnrichDetail(detail string, res enrichResult, model string) string {
	m := map[string]any{}
	if detail != "" {
		_ = jsonx.Unmarshal([]byte(detail), &m)
	}
	setDetailStrIfAbsent(m, "microstructure_mechanism", compactText(string(res.MicrostructureMechanism)))
	setDetailStrIfAbsent(m, "composition_change", compactText(string(res.CompositionChange)))
	setDetailStrIfAbsent(m, "process_change", compactText(string(res.ProcessChange)))
	setDetailSliceIfAbsent(m, "characterization_methods", compactSlice(res.CharacterizationMethods))
	setDetailSliceIfAbsent(m, "test_methods", compactSlice(res.TestMethods))
	setDetailSliceIfAbsent(m, "failure_modes", compactSlice(res.FailureModes))
	if _, ok := m["causal_chain"]; !ok {
		if chain := cleanCausalChain(res.CausalChain); len(chain) > 0 {
			m["causal_chain"] = chain
		}
	}
	if _, ok := m["experiment_plan"]; !ok {
		if plan := cleanExperiment(res.Experiment); plan != nil {
			m["experiment_plan"] = plan
		}
	}
	if _, ok := m["feasibility"]; !ok {
		if fz := cleanFeasibility(res.Feasibility); len(fz) > 0 {
			m["feasibility"] = fz
		}
	}
	m["enrichment"] = map[string]any{keyModel: model, "enriched_at": time.Now().UTC().Format(time.RFC3339)}
	b, err := jsonx.Marshal(m)
	if err != nil {
		return detail
	}
	return string(b)
}

func setDetailStrIfAbsent(m map[string]any, key, val string) {
	if val == "" {
		return
	}
	if cur, ok := m[key]; ok {
		if s, isStr := cur.(string); !isStr || strings.TrimSpace(s) != "" {
			return
		}
	}
	m[key] = val
}

func setDetailSliceIfAbsent(m map[string]any, key string, val []string) {
	if len(val) == 0 {
		return
	}
	if cur, ok := m[key]; ok {
		if arr, isArr := cur.([]any); isArr && len(arr) > 0 {
			return
		}
	}
	m[key] = val
}

// mergeEnrichAssessment ставит метку assessment.enrichment: по ней EnsureEnrich видит, что обогащение уже было.
func mergeEnrichAssessment(assessment string, score float64, missing []string, model string) string {
	m := map[string]any{}
	if assessment != "" {
		_ = jsonx.Unmarshal([]byte(assessment), &m)
	}
	m["schema"] = map[string]any{keyScore: score, "complete": len(missing) == 0, keyMissing: missing}
	m["enrichment"] = map[string]any{keyModel: model, "enriched_at": time.Now().UTC().Format(time.RFC3339)}
	b, err := jsonx.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(b)
}

// detailSignals считает полноту через schemaCompleteness, чтобы фильтр и доска не расходились.
func detailSignals(detail string, measurable bool) (float64, []string, bool, bool) {
	m := map[string]any{}
	if detail != "" {
		_ = jsonx.Unmarshal([]byte(detail), &m)
	}
	item := genItem{
		MaterialSystem:          mapString(m, "material_system"),
		CompositionChange:       mapString(m, "composition_change"),
		ProcessChange:           mapString(m, "process_change"),
		MicrostructureMechanism: mapString(m, "microstructure_mechanism"),
		TargetProperty:          mapString(m, "target_property"),
	}
	if arr, ok := m["causal_chain"].([]any); ok && len(arr) > 0 {
		item.CausalChain = []causalStep{{}} // schemaCompleteness смотрит только на len>0
	}
	if plan, ok := m["experiment_plan"].(map[string]any); ok && planHasSuccessCriteria(plan) {
		item.Experiment = &experimentVal{SuccessCriteria: "set"}
	}
	score, missing := schemaCompleteness(item, measurable)
	hasMechanism := item.MicrostructureMechanism != "" || len(item.CausalChain) > 0
	hasPlan := item.Experiment != nil && item.Experiment.SuccessCriteria != ""
	return score, missing, hasMechanism, hasPlan
}

// planHasSuccessCriteria: генерация хранит критерий строкой, планировщик — массивом.
func planHasSuccessCriteria(plan map[string]any) bool {
	switch v := plan["success_criteria"].(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	default:
		return false
	}
}

func mapString(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func hasGroundedEvidence(h *commonv1.Hypothesis) bool {
	for _, e := range h.GetEvidence() {
		if strings.TrimSpace(e.GetDocumentId()) != "" {
			return true
		}
	}
	return false
}

func qualityGate(score float64, hasMechanism, hasPlan, grounded, duplicate bool, floor float64) (string, bool) {
	switch {
	case duplicate:
		return "duplicate statement", true
	case score < floor && !hasMechanism && !hasPlan:
		return "low schema completeness and no mechanism/plan after enrichment", true
	case !grounded && score < floor:
		return "no grounded evidence and low schema completeness", true
	default:
		return "", false
	}
}

func archiveReasonRU(reason string) string {
	switch reason {
	case "duplicate statement":
		return "дубликат формулировки"
	case "low schema completeness and no mechanism/plan after enrichment":
		return "низкая полнота и нет механизма/плана"
	case "no grounded evidence and low schema completeness":
		return "нет привязки к документам и низкая полнота"
	default:
		return reason
	}
}

func (s *HypothesisService) isDuplicateStatement(ctx context.Context, ownerID, id, statement string) bool {
	norm := normalizedStatement(statement)
	if norm == "" {
		return false
	}
	found := false
	err := s.walkHypotheses(ctx, &dbv1.ListHypothesesRequest{OwnerId: ownerID}, func(hs []*commonv1.Hypothesis) (bool, error) {
		for _, other := range hs {
			if other.GetId() == id {
				continue
			}
			if other.GetStatus() == statusArchived || other.GetStatus() == statusRejected {
				continue
			}
			if normalizedStatement(other.GetStatement()) != norm {
				continue
			}
			// Дубль — только если у близнеца id меньше: выживает наименьший id, и две одинаковые гипотезы,
			// обогащаемые параллельно, не архивируют друг друга.
			if other.GetId() < id {
				found = true
				return false, nil
			}
		}
		return true, nil
	})
	return err == nil && found
}

func normalizedStatement(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func enrichmentDone(assessment string) bool {
	if assessment == "" {
		return false
	}
	var m struct {
		Enrichment struct {
			EnrichedAt string `json:"enriched_at"`
		} `json:"enrichment"`
	}
	if err := jsonx.Unmarshal([]byte(assessment), &m); err != nil {
		return false
	}
	return m.Enrichment.EnrichedAt != ""
}
