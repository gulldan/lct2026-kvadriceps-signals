package application

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
	"github.com/example/main-service/internal/platform/llmusage"
)

type liveLLMRunKey struct{}
type liveRunDeadlineKey struct{}

// liveCallReserve — меньше этого до конца прогона новый вызов не начинается: срок оборвал бы его посреди
// генерации. Замер 09.10: 44 из 70 пересчётов упирались в срок и рвали по 8–16 вызовов длиной 2–46 с,
// это 3,4 % времени генерации радара, а шлюз видел пачку закрытых соединений каждые 12 минут.
const liveCallReserve = time.Minute

// errLiveRunEnding оборачивает истечение срока: для вызывающего кода пропуск неотличим от оборванного
// вызова, и ветки «срок вышел» срабатывают так же.
var errLiveRunEnding = fmt.Errorf("до конца прогона меньше %s: %w", liveCallReserve, context.DeadlineExceeded)

// liveRunBudget ставит прогону общий срок и запоминает его: внутри вызова срок контекста — уже свой
// таймаут вызова, и по нему не понять, сколько осталось прогону.
func liveRunBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, liveSearchBudget)
	deadline, _ := ctx.Deadline()
	return context.WithValue(ctx, liveRunDeadlineKey{}, deadline), cancel
}

func liveRunEnding(ctx context.Context) bool {
	deadline, bounded := ctx.Value(liveRunDeadlineKey{}).(time.Time)
	return bounded && time.Until(deadline) < liveCallReserve
}

type liveAuditedGenerator struct {
	aiclients.Generator
	requestedModel func(context.Context) string
	usage          llmusage.KV
}

// NewLiveAuditedGenerator журналирует каждый вызов радара без ключей, исходных
// документов и пользовательского запроса. Хеш связывает вызов с версией промпта.
// Токены идут и в общий реестр расхода: иначе страница метрик не видит самого дорогого потребителя.
func NewLiveAuditedGenerator(
	generator aiclients.Generator, requestedModel func(context.Context) string, usage llmusage.KV,
) aiclients.Generator {
	return liveAuditedGenerator{Generator: generator, requestedModel: requestedModel, usage: usage}
}

func (g liveAuditedGenerator) Generate(ctx context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	if liveRunEnding(ctx) {
		return aiclients.GenResponse{}, fmt.Errorf("radar llm generate: %w", errLiveRunEnding)
	}
	started := time.Now()
	requestedModel := g.requestedModel(ctx)
	response, err := g.Generator.Generate(ctx, request)
	liveRecordLLM(ctx, g.usage, requestedModel, request, response, err == nil, time.Since(started))
	if err != nil {
		return response, fmt.Errorf("radar llm generate: %w", err)
	}
	return response, nil
}

func (g liveAuditedGenerator) Stream(ctx context.Context, request aiclients.GenRequest,
	onToken func(string) error,
) (aiclients.GenResponse, error) {
	if liveRunEnding(ctx) {
		return aiclients.GenResponse{}, fmt.Errorf("radar llm stream: %w", errLiveRunEnding)
	}
	started := time.Now()
	requestedModel := g.requestedModel(ctx)
	response, err := g.Generator.Stream(ctx, request, onToken)
	liveRecordLLM(ctx, g.usage, requestedModel, request, response, err == nil, time.Since(started))
	if err != nil {
		return response, fmt.Errorf("radar llm stream: %w", err)
	}
	return response, nil
}

func liveRecordLLM(ctx context.Context, kv llmusage.KV, requestedModel string, request aiclients.GenRequest,
	response aiclients.GenResponse, succeeded bool, elapsed time.Duration,
) {
	runID, _ := ctx.Value(liveLLMRunKey{}).(string)
	if runID == "" {
		runID = rand.Text()
	}
	operation := liveLLMOperation(request.System)
	liveTallyOf(ctx).addLLM(operation, request, response, succeeded, elapsed)
	usage := response.Usage
	reported := usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0
	attrs := []slog.Attr{
		slog.String("run_id", runID), slog.String("operation", operation),
		slog.String("prompt_sha256", fmt.Sprintf("%x", sha256.Sum256([]byte(request.System)))),
		slog.String("requested_model", requestedModel),
		slog.String("model", response.Model), slog.Bool("succeeded", succeeded),
		slog.Int64("duration_ms", elapsed.Milliseconds()), slog.Bool("usage_reported", reported),
	}
	// Нули без учёта — не измерение: неизвестный расход не означает бесплатный вызов.
	if reported {
		attrs = append(attrs, slog.Int("prompt_tokens", usage.PromptTokens),
			slog.Int("completion_tokens", usage.CompletionTokens), slog.Int("total_tokens", usage.TotalTokens))
		_ = llmusage.Record(ctx, kv, cmp.Or(response.Model, requestedModel), operation,
			usage.PromptTokens, usage.CompletionTokens, 0)
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "radar_llm_call", attrs...)
}

func liveLLMOperation(system string) string {
	switch system {
	case searchTermsSystem, liveSearchTermsSystem:
		return "translate_query"
	case topicNameSystem:
		return "name_candidate"
	case liveQualificationSystem:
		return "qualify_candidate"
	case liveQualificationReviewSystem:
		return "review_candidate"
	case liveCardSystem, liveCardSystem + liveQueryScopeInstruction:
		return "write_card"
	case liveCardVerifySystem, liveCardVerifySystem + liveQueryScopeInstruction:
		return "verify_card"
	case liveDigestSystem, liveDigestSystem + " " + liveDigestRussianOnly:
		return "interpret_source"
	case liveExtractSystem, liveExtractSystem + liveQueryScopeInstruction:
		return "extract_candidates"
	case liveExtractMarketSystem + liveQueryScopeInstruction:
		return "extract_market_candidates"
	case liveExecutiveSystem:
		return "executive_brief"
	case liveSignalBriefSystem:
		return "signal_brief"
	case liveRatingSystem:
		return "rate_maturity"
	case liveEventCheckSystem:
		return "verify_events"
	case weakSignalPhrasesSystem:
		return "phrase_technology"
	}
	return "unclassified"
}
