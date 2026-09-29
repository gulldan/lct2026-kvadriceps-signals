package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"google.golang.org/protobuf/proto"
)

const (
	genMinCount    = 3
	genMaxCount    = 10
	genLLMAttempts = 3
	// genEvidenceTop с запасом: llm-service переранжирует, и глубже выборка лишь точнее отбирает свидетельства.
	genEvidenceTop        = 16
	genEvidenceTopMax     = 24
	genPerHypEvidence     = 6 // разных документов-свидетельств на гипотезу
	genEvidenceChunks     = 2
	genCounterTop         = 6 // источников-контраргументов на прогон
	genFeedbackTitles     = 4 // отклонённых и одобренных заголовков в промпт
	genPersistConcurrency = 6
	genFeedbackFetch      = 12 // последних гипотез каждого статуса для заметки об отзывах
	genPromptVer          = "gen-v6"
	keyRationale          = "rationale"
	keyModel              = "model"
	keyMethod             = "method"
	keyLevel              = "level"
	keyName               = "name"
	keyItems              = "items"
	editorSystem          = "system"
	levelLow              = "low"
	levelMedium           = "medium"
	levelHigh             = "high"
	actionCreated         = "created"
	actionEdited          = "edited"
	actionScoreOverride   = "score_override"
	statusRejected        = "rejected"
	statusApproved        = "approved"
	statusGenerated       = "generated"
	methodSEM             = "SEM"
	methodXRD             = "XRD"
)

// adaptiveTopK: короткий общий запрос охватывает широкое поле и получает выборку глубже, конкретный — базовую.
func adaptiveTopK(query string, base, hi int32) int32 {
	terms := len(tokenSet(query)) // разные содержательные токены от 4 рун
	switch {
	case terms <= 4:
		return hi
	case terms <= 8:
		return base + (hi-base)/2
	default:
		return base
	}
}

func (s *HypothesisService) Generate(
	ctx context.Context, ownerID string, kpi *commonv1.KPI, count int, constraints string,
) ([]*commonv1.Hypothesis, error) {
	return s.generate(ctx, ownerID, kpi, count, constraints, nil)
}

// GenerateFromDocuments: пустой docIDs означает весь корпус.
func (s *HypothesisService) GenerateFromDocuments(
	ctx context.Context, ownerID string, kpi *commonv1.KPI, count int, constraints string, docIDs []string,
) ([]*commonv1.Hypothesis, error) {
	return s.generate(ctx, ownerID, kpi, count, constraints, docIDs)
}

func (s *HypothesisService) generate(
	ctx context.Context, ownerID string, kpi *commonv1.KPI, count int, constraints string, scopeDocIDs []string,
) ([]*commonv1.Hypothesis, error) {
	settings := s.runtimeSettings(ctx, ownerID)
	count = generateCount(count, settings)
	constraints = compactText(constraints)
	prepared := s.prepareGeneration(ctx, ownerID, kpi, count, constraints, scopeDocIDs, settings)
	llmBudget := time.Duration(settings.GenerationTimeoutSec) * time.Second
	llmCtx, cancel := withDeadline(ctx, llmBudget)
	defer cancel()

	gen := s.generateItems(llmCtx, count, prepared.request)
	if len(gen.items) == 0 {
		if gen.lastErr != nil {
			return nil, fmt.Errorf("не удалось сгенерировать обоснованные гипотезы; повторите запрос: %w", gen.lastErr)
		}
		return nil, errors.New("в найденных источниках недостаточно данных для гипотез; уточните тезис или добавьте документы")
	}
	if len(gen.items) > count {
		gen.items = gen.items[:count]
	}

	// Повторная оценка вторым проходом LLM, чтобы в оценке не осталась самооценка генератора; nil — остаются свои оценки.
	deepPostprocess := settings.DeepPostprocessEnabled
	var judged map[int]scoreSet
	if deepPostprocess {
		judged = s.judgeScores(llmCtx, ownerID, kpi, gen.items, prepared.request.ExcludeDocumentIds)
	}

	sources := gen.resp.GetSources()
	// Контраргументы — чтобы отбор свидетельств видел противоречия, а не только найденное запросом KPI.
	if deepPostprocess {
		sources = append(sources, s.counterEvidenceSources(
			llmCtx, ownerID, prepared.request.Query, genCounterTop, prepared.request.ExcludeDocumentIds,
		)...)
	}
	// Документы цели идут в пул как цитируемые «входные данные»; поиск выше их исключил, за мировую практику они не выдаются.
	sources = append(sources, prepared.inputSources...)
	return s.persistGenerated(
		ctx, ownerID, kpi, gen, judged, sources, deepPostprocess, constraints, prepared.works, prepared.inputIDs,
	)
}

func generateCount(count int, settings HypothesisRuntimeSettings) int {
	if count <= 0 {
		count = settings.DefaultGenerateCount
	}
	return max(genMinCount, min(count, genMaxCount))
}

// Источники принадлежат именно выбранному ответу, а не последней неудачной попытке.
type genOutcome struct {
	items   []genItem
	resp    *commonv1.RagResponse
	lastErr error
}

func (s *HypothesisService) generateItems(
	ctx context.Context, count int, request *commonv1.RagRequest,
) genOutcome {
	minWanted := min(count, genMinCount)
	var out genOutcome
	for attempt := range genLLMAttempts {
		if ctx.Err() != nil {
			out.lastErr = ctx.Err()
			break
		}
		resp, err := s.generateAttempt(ctx, request, attempt)
		if err != nil {
			out.lastErr = err
			// Транспорт уже исчерпал свои повторы; новый вызов умножил бы их число.
			return out
		}
		if isStubGeneration(resp) {
			out.lastErr = errors.New("модель генерации не настроена")
			return out
		}
		if out.absorb(resp, minWanted) {
			return out
		}
	}
	return out
}

func (s *HypothesisService) generateAttempt(
	ctx context.Context, request *commonv1.RagRequest, attempt int,
) (*commonv1.RagResponse, error) {
	q := proto.CloneOf(request)
	q.Prompt = generationPromptAttempt(request.GetPrompt(), attempt)
	// Повторы исправляют форму ответа. Деление общего бюджета заставляло трижды отменять
	// исправную, но медленную генерацию на одинаковом месте.
	return s.answerer.Answer(opCtx(ctx, "generate"), q)
}

func (out *genOutcome) absorb(resp *commonv1.RagResponse, minWanted int) bool {
	items, perr := parseGenItems(resp.GetAnswer())
	if perr != nil {
		out.lastErr = perr
		return false
	}
	// Пустой массив — осмысленный отказ при недостатке свидетельств, а не сломанный ответ.
	if len(items) == 0 {
		out.lastErr = nil
		return true
	}
	if usable := usableGenItems(items); len(usable) > len(out.items) {
		out.items = usable
		out.resp = resp
	}
	if len(out.items) >= minWanted {
		return true
	}
	if len(out.items) == 0 {
		out.lastErr = errors.New("model returned an empty hypothesis array")
	}
	return false
}
