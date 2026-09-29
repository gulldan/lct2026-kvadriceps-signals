package aiadapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/aiclients"
)

// Модели с рассуждением (DeepSeek) тратят тысячи скрытых токенов из общего max_tokens до ответа:
// при 4096 структурная генерация гипотез обрывалась с пустым текстом.
func genMaxTokens() int {
	if v, err := strconv.Atoi(os.Getenv("RAG_GEN_MAX_TOKENS")); err == nil && v > 0 {
		return v
	}
	return 12288
}

type Embedder struct {
	client aiclients.Embedder
}

func NewEmbedder(client aiclients.Embedder) *Embedder {
	return &Embedder{client: client}
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.client.Embed(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(vecs) == 0 {
		return nil, errors.New("embed: no vector returned")
	}
	return vecs[0], nil
}

type Reranker struct {
	client aiclients.Reranker
}

func NewReranker(client aiclients.Reranker) *Reranker {
	return &Reranker{client: client}
}

func (r *Reranker) Rank(ctx context.Context, query string, chunks []domain.RetrievedChunk) ([]float64, error) {
	docs := make([]string, len(chunks))
	for i, c := range chunks {
		docs[i] = c.Text
	}
	scores, err := r.client.Rerank(ctx, query, docs)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	out := make([]float64, len(scores))
	for i, s := range scores {
		out[i] = float64(s)
	}
	return out, nil
}

// Reasoner идёт мимо обёртки вопроса по контексту (без инструкции ссылок), чтобы промпт целиком задавал контроллер.
type Reasoner struct {
	client    aiclients.Generator
	maxTokens int
}

func NewReasoner(client aiclients.Generator, maxTokens int) *Reasoner {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	return &Reasoner{client: client, maxTokens: maxTokens}
}

// Reason держит низкую температуру: решение «искать или стоп» должно быть устойчивым.
func (r *Reasoner) Reason(ctx context.Context, system, user string) (string, error) {
	resp, err := r.client.Generate(ctx, aiclients.GenRequest{
		System:      system,
		Context:     "",
		Query:       user,
		MaxTokens:   r.maxTokens,
		Temperature: 0.1,
		Cite:        false,
	})
	if err != nil {
		return "", fmt.Errorf("reason: %w", err)
	}
	return resp.Text, nil
}

// UsageRecorder не должен блокировать или ронять генерацию: учёт расхода — по возможности.
type UsageRecorder interface {
	Record(ctx context.Context, model, operation string, promptTokens, completionTokens int, costUSD float64)
}

type Generator struct {
	client  aiclients.Generator
	timeout time.Duration
	usage   UsageRecorder
}

func NewGenerator(client aiclients.Generator) *Generator {
	return &Generator{client: client}
}

func (g *Generator) WithUsage(rec UsageRecorder) *Generator {
	g.usage = rec
	return g
}

// Метку операции ставит main-service в gRPC-метаданных (generate, verify, enrich…).
func operationFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("operation"); len(v) > 0 && v[0] != "" {
			return v[0]
		}
	}
	return "answer"
}

// NewGeneratorWithTimeout ограничивает весь вызов: при хвостах задержки провайдера
// чат успевает отдать выдержки с источниками.
func NewGeneratorWithTimeout(client aiclients.Generator, timeout time.Duration) *Generator {
	return &Generator{client: client, timeout: timeout}
}

func (g *Generator) Answer(ctx context.Context, contextText, query string, cite bool) (string, string, error) {
	if g.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	maxTokens := 1024
	temperature := float32(0.0)
	if !cite {
		maxTokens = genMaxTokens()
		temperature = 0.2
	}
	resp, err := g.client.Generate(ctx, aiclients.GenRequest{
		Context:     contextText,
		Query:       query,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Cite:        cite,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate: %w", err)
	}
	if g.usage != nil {
		g.usage.Record(ctx, resp.Model, operationFromContext(ctx),
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CostUSD)
	}
	return resp.Text, resp.Model, nil
}
