package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

type liveLLMRunKey struct{}
type liveAuditedGenerator struct {
	aiclients.Generator
	requestedModel func(context.Context) string
}

// NewLiveAuditedGenerator журналирует каждый вызов радара без ключей, исходных
// документов и пользовательского запроса. Хеш связывает вызов с версией промпта.
func NewLiveAuditedGenerator(generator aiclients.Generator, requestedModel func(context.Context) string) aiclients.Generator {
	return liveAuditedGenerator{Generator: generator, requestedModel: requestedModel}
}

func (g liveAuditedGenerator) Generate(ctx context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	started := time.Now()
	requestedModel := g.requestedModel(ctx)
	response, err := g.Generator.Generate(ctx, request)
	liveRecordLLM(ctx, requestedModel, request, response, err == nil, time.Since(started))
	if err != nil {
		return response, fmt.Errorf("radar llm generate: %w", err)
	}
	return response, nil
}

func (g liveAuditedGenerator) Stream(ctx context.Context, request aiclients.GenRequest,
	onToken func(string) error,
) (aiclients.GenResponse, error) {
	started := time.Now()
	requestedModel := g.requestedModel(ctx)
	response, err := g.Generator.Stream(ctx, request, onToken)
	liveRecordLLM(ctx, requestedModel, request, response, err == nil, time.Since(started))
	if err != nil {
		return response, fmt.Errorf("radar llm stream: %w", err)
	}
	return response, nil
}

func liveRecordLLM(ctx context.Context, requestedModel string, request aiclients.GenRequest, response aiclients.GenResponse,
	succeeded bool, elapsed time.Duration,
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
