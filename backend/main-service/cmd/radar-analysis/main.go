// radar-analysis только анализирует: сбор и публикация остаются за radar-collector, поэтому упавший
// анализ не затрёт удачное наблюдение.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/example/main-service/internal/platform/openalex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/infrastructure/trendsemantic"
	"github.com/example/main-service/internal/platform/config"
)

type metadataEnricher interface {
	Enrich(context.Context, application.LiveAnalysisRequest, int) (map[string]application.LivePaperMetadata, []application.LiveLimit)
}

func analysisHandler(encoder application.LiveSemanticEncoder, enrichers ...metadataEnricher) http.Handler {
	mux := http.NewServeMux()
	busy := make(chan struct{}, 1)
	// Тело пустое намеренно: этот же адрес служит целью Prometheus, а «ok» —
	// невалидная экспозиция, и анализатор из-за неё не наблюдался вовсе.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /v1/analyze", exclusiveAnalysis(busy, func(w http.ResponseWriter, r *http.Request) {
		analyzeRequest(w, r, encoder, enrichers)
	}))
	mux.HandleFunc("POST /v1/evaluate", exclusiveAnalysis(busy, func(w http.ResponseWriter, r *http.Request) {
		evaluateRequest(w, r, encoder)
	}))
	return mux
}

func exclusiveAnalysis(busy chan struct{}, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
			handler(w, r)
		default:
			http.Error(w, "analysis already running", http.StatusConflict)
		}
	}
}

func analyzeRequest(
	w http.ResponseWriter, r *http.Request, encoder application.LiveSemanticEncoder, enrichers []metadataEnricher,
) {
	// Выборка окна по всем разделам arXiv с аннотациями — десятки мегабайт;
	// предел держит один запрос в памяти сервиса, а не защищает от него.
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	var input application.LiveAnalysisRequest
	if err := json.UnmarshalRead(r.Body, &input, json.RejectUnknownMembers(true)); err != nil {
		http.Error(w, "invalid analysis request", http.StatusBadRequest)
		return
	}
	// Предел удерживает память сервиса, а не размер науки: снимок курируемых направлений
	// несёт больше пятидесяти тысяч работ, а разбор берёт из него ограниченную выборку.
	if len(input.Snapshot.Papers) > 200000 || len(input.Metadata) > 600000 ||
		len(input.DiscoveryPapers) > application.LiveDiscoveryRequestLimit {
		http.Error(w, "request exceeds paper budget", http.StatusRequestEntityTooLarge)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Minute)
	defer cancel()
	metadataLimits := []application.LiveLimit{}
	if input.EnrichMetadata && !input.EvidenceOnly {
		for _, enricher := range enrichers {
			var limits []application.LiveLimit
			input.Metadata, limits = enricher.Enrich(ctx, input, 64)
			metadataLimits = append(metadataLimits, limits...)
		}
	}
	if !input.StrictAvailability && (input.EnrichMetadata || input.EvidenceOnly) {
		// Живые доказательства наблюдаются после сбора; состав статей и окно публикаций сохраняют исходную отсечку.
		input.EvidenceObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	result, err := application.AnalyzeLive(ctx, encoder, input)
	if err != nil {
		slog.Warn("radar analysis failed", "error", err)
		http.Error(w, "analysis unavailable; retain the previous observation", http.StatusBadGateway)
		return
	}
	if input.EnrichMetadata && !input.EvidenceOnly {
		result.Metadata = input.Metadata
	}
	result.Limits = append(result.Limits, metadataLimits...)
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, result, json.Deterministic(true)); err != nil {
		slog.Warn("write analysis response", "error", err)
	}
}

func evaluateRequest(w http.ResponseWriter, r *http.Request, encoder application.LiveSemanticEncoder) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var input struct {
		Pairs     []application.LiveSemanticPair `json:"pairs"`
		Threshold float64                        `json:"threshold"`
	}
	if err := json.UnmarshalRead(r.Body, &input, json.RejectUnknownMembers(true)); err != nil {
		http.Error(w, "invalid evaluation request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Minute)
	defer cancel()
	result, err := application.EvaluateLiveSemanticPairs(ctx, encoder, input.Pairs, input.Threshold)
	if err != nil {
		http.Error(w, "evaluation unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, result, json.Deterministic(true)); err != nil {
		slog.Warn("write evaluation response", "error", err)
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("radar analysis stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "evaluate-burst", "calibrate-burst":
			return diagnoseBurst(args[0], os.Stdin, os.Stdout)
		case "healthcheck":
			return healthcheck(context.Background())
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := trendsemantic.New(
		config.Get("EMBEDDINGS_URL", "http://f2llm-service:8085/v1/embeddings"),
		config.Get("EMBEDDINGS_MODEL", "codefuse-ai/F2LLM-v2-0.6B"),
	)
	oa, err := openalex.New(openalex.FromEnv("rag-test-radar-analysis/1.0"))
	if err != nil {
		return fmt.Errorf("openalex client: %w", err)
	}
	affiliations := trendsemantic.NewOpenAlex(oa)
	lineage := trendsemantic.NewGitHubLineage("https://api.github.com", os.Getenv("GITHUB_TOKEN"))
	server := &http.Server{
		Addr: config.Get("RADAR_ANALYSIS_ADDR", ":8080"), Handler: analysisHandler(encoder, affiliations, lineage),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 15 * time.Minute, IdleTimeout: time.Minute,
	}
	return serveAnalysis(ctx, server)
}

func diagnoseBurst(action string, input io.Reader, output io.Writer) error {
	var snapshot application.LiveSnapshot
	if err := json.UnmarshalRead(io.LimitReader(input, 64<<20), &snapshot, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("read burst evaluation input: %w", err)
	}
	var result any
	var err error
	if action == "calibrate-burst" {
		result, err = application.CalibrateLiveBurst(snapshot)
	} else {
		result, err = application.EvaluateLiveBurst(snapshot)
	}
	if err != nil {
		return fmt.Errorf("evaluate dated burst: %w", err)
	}
	if err := json.MarshalWrite(output, result, json.Deterministic(true)); err != nil {
		return fmt.Errorf("write burst evaluation: %w", err)
	}
	return nil
}

func healthcheck(ctx context.Context) error {
	client := &http.Client{Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8080/healthz", http.NoBody)
	if err != nil {
		return fmt.Errorf("create healthcheck request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck status: %d", response.StatusCode)
	}
	return nil
}

func serveAnalysis(ctx context.Context, server *http.Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopped := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer stop()
		stopped <- server.Shutdown(shutdown)
	}()
	slog.Info("radar analysis listening", "address", server.Addr, "method", application.LiveAnalysisVersion)
	serveErr := server.ListenAndServe()
	cancel()
	shutdownErr := <-stopped
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve radar analysis: %w", serveErr)
	}
	if shutdownErr != nil {
		return fmt.Errorf("shutdown radar analysis: %w", shutdownErr)
	}
	return nil
}
