package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/llm-service/internal/application"
	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/infrastructure/aiadapters"
	"github.com/example/llm-service/internal/infrastructure/cache"
	"github.com/example/llm-service/internal/infrastructure/graph"
	"github.com/example/llm-service/internal/infrastructure/load"
	"github.com/example/llm-service/internal/infrastructure/retrieval"
	"github.com/example/llm-service/internal/interfaces/grpcserver"
	"github.com/example/llm-service/internal/platform/aiclients"
	"github.com/example/llm-service/internal/platform/config"
	"github.com/example/llm-service/internal/platform/dbclient"
	"github.com/example/llm-service/internal/platform/grpcx"
	"github.com/example/llm-service/internal/platform/llmusage"
	"github.com/example/llm-service/internal/platform/logger"
	"github.com/example/llm-service/internal/platform/observability"
	"github.com/example/llm-service/internal/platform/runtimecfg"
	"github.com/example/llm-service/internal/platform/searchstore"
	"github.com/example/llm-service/internal/platform/valkey"
	"github.com/example/llm-service/internal/platform/vectorstore"

	graphv1 "github.com/example/llm-service/internal/platform/genproto/graph/v1"
	llmv1 "github.com/example/llm-service/internal/platform/genproto/llm/v1"
)

type usageRecorder struct{ kv *valkey.Client }

func (u usageRecorder) Record(ctx context.Context, model, operation string, promptTokens, completionTokens int, costUSD float64) {
	_ = llmusage.Record(ctx, u.kv, model, operation, promptTokens, completionTokens, costUSD)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(config.Get("METRICS_ADDR", ":9090")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	log := logger.New("llm-service", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("llm-service stopped with error")
		os.Exit(1)
	}
}

func healthcheck(metricsAddr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL(metricsAddr), nil)
	if err != nil {
		return fmt.Errorf("healthcheck request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("readyz: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("readyz status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func readyURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + "/readyz"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	} else if host, port, err := net.SplitHostPort(addr); err == nil {
		switch host {
		case "", "0.0.0.0", "::", "[::]":
			addr = net.JoinHostPort("127.0.0.1", port)
		}
	}
	return "http://" + addr + "/readyz"
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Пустой URL включает детерминированную заглушку: конвейер работает целиком без GPU.
	embedder := aiclients.NewEmbedder(
		config.Get("EMBEDDINGS_URL", ""),
		config.Get("EMBEDDINGS_MODEL", ""),
		config.GetInt("EMBEDDING_DIM", 1024),
		nil,
	)
	rerankerURL := config.Get("RERANKER_URL", "")
	reranker := aiclients.NewReranker(
		rerankerURL,
		config.Get("RERANKER_MODEL", ""),
		nil,
	)

	// Коллекция, размерность и индекс обязаны совпадать с chunk-splitter: создаёт их он, здесь только чтение.
	qdrant := vectorstore.NewQdrant(
		config.Get("QDRANT_URL", "http://qdrant:6333"),
		config.Get("QDRANT_COLLECTION", "documents"),
	)
	opensearch := searchstore.NewOpenSearch(
		config.Get("OPENSEARCH_URL", "http://opensearch:9200"),
		config.Get("OPENSEARCH_INDEX", "chunks"),
	)

	valkeyClient, err := valkey.New(ctx, config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""), 0)
	if err != nil {
		return err
	}
	defer func() { _ = valkeyClient.Close() }()

	answerCache := cache.NewAnswerCache(valkeyClient, config.GetDuration("ANSWER_CACHE_TTL", time.Hour))

	// Бэкенд генерации читается на каждый вызов: смена VLLM_* из админки применяется без перезапуска.
	ovr := runtimecfg.New(valkeyClient)
	generator := aiclients.NewDynamicGenerator(func(ctx context.Context) aiclients.GenTarget {
		return aiclients.GenTarget{
			URL:             ovr.Get(ctx, "VLLM_URL", ""),
			Model:           ovr.Get(ctx, "VLLM_MODEL", ""),
			ReasoningEffort: ovr.Get(ctx, "VLLM_REASONING_EFFORT", ""),
			APIKey:          ovr.Get(ctx, "VLLM_API_KEY", ""),
			RPM:             ovr.GetInt(ctx, "VLLM_RPM", 0),
		}
	}, nil)

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, readiness(qdrant, opensearch), log)
	}()

	// 0.5 — как в compose, под нормированные 0..1 баллы Qwen3-Reranker.
	scoreFloor := config.GetFloat("RAG_SCORE_FLOOR", 0.5)
	log.Info().Float64("score_floor", scoreFloor).Bool("reranker", rerankerURL != "").
		Msg("abstain score floor configured")

	svc := application.New(
		aiadapters.NewEmbedder(embedder),
		retrieval.NewQdrant(qdrant),
		retrieval.NewOpenSearch(opensearch),
		aiadapters.NewReranker(reranker),
		aiadapters.NewGeneratorWithTimeout(generator, config.GetDuration("RAG_GENERATE_TIMEOUT", 150*time.Second)).
			WithUsage(usageRecorder{kv: valkeyClient}),
		answerCache,
		retrieval.NewChunkReader(qdrant),
		load.New(valkeyClient),
		metrics,
		config.GetInt("RAG_TOP_K", 5),
		config.GetBool("RAG_SHARED_CORPUS", true),
		scoreFloor,
		rerankerURL != "",
		application.Tuning{
			// Потолок на документ: доказательства из разных источников, а не почти одинаковые куски одного.
			MaxPerDoc:     config.GetInt("RAG_MAX_PER_DOC", 4),
			VecWeight:     config.GetFloat("RAG_VEC_WEIGHT", 1),
			LexWeight:     config.GetFloat("RAG_LEX_WEIGHT", 1),
			RetrievalMult: config.GetInt("RAG_RETRIEVAL_MULT", 4),
			RetrievalMin:  config.GetInt("RAG_RETRIEVAL_MIN", 20),
		},
	)

	configureCacheRevision(svc, ovr)
	db, err := dbclient.New(config.Get("DB_SERVICE_ADDR", "db-service:9091"))
	if err != nil {
		return fmt.Errorf("source index client: %w", err)
	}
	defer func() { _ = db.Close() }()
	svc.EnableSourceReads(db, config.GetBool("SOURCE_INDEX_SNAPSHOTS", true))

	if cleanup := configureAgentic(log, svc, ovr); cleanup != nil {
		defer cleanup()
	}

	srv := grpcx.NewServer(log)
	serviceServer := grpcserver.New(
		svc,
		config.GetInt("RAG_MAX_CONCURRENT", 16),
		config.GetDuration("RAG_QUEUE_WAIT", 10*time.Second),
	)
	llmv1.RegisterRagServiceServer(srv.Registrar(), serviceServer)
	stopDescriptions := startDescriptions(ctx, serviceServer, db, generator, usageRecorder{kv: valkeyClient})
	defer stopDescriptions()
	return srv.Run(ctx, config.Get("GRPC_ADDR", ":9091"))
}

func configureAgentic(log zerolog.Logger, svc *application.RAGService, ovr *runtimecfg.Overrides) func() {
	if !config.GetBool("RAG_AGENTIC", false) {
		return nil
	}
	reasonerGen := aiclients.NewDynamicGenerator(func(ctx context.Context) aiclients.GenTarget {
		return aiclients.GenTarget{
			URL:             ovr.Get(ctx, "VLLM_URL", ""),
			Model:           ovr.Get(ctx, "RAG_REASONER_MODEL", ovr.Get(ctx, "VLLM_MODEL", "")),
			ReasoningEffort: ovr.Get(ctx, "VLLM_REASONING_EFFORT", ""),
			APIKey:          ovr.Get(ctx, "VLLM_API_KEY", ""),
			RPM:             ovr.GetInt(ctx, "VLLM_RPM", 0),
		}
	}, nil)

	var graphExp domain.GraphExpander
	var cleanup func()
	if config.GetBool("RAG_AGENTIC_GRAPH", false) {
		addr := config.Get("GRAPH_COMPUTE_ADDR", "graph-compute:9093")
		conn, err := grpcx.Dial(addr)
		if err != nil {
			log.Warn().Err(err).Str("addr", addr).Msg("agentic graph dial failed; graph tool disabled")
		} else {
			cleanup = func() { _ = conn.Close() }
			graphExp = graph.NewClient(
				graphv1.NewGraphComputeClient(conn),
				config.Get("QDRANT_COLLECTION", "documents"),
				// Не GRAPH_COMPUTE_TIMEOUT: у Rust-воркеров там секунды числом, а этому клиенту нужна длительность («10s»).
				config.GetDuration("RAG_GRAPH_COMPUTE_TIMEOUT", 10*time.Second),
			)
		}
	}

	maxHops := config.GetInt("RAG_AGENTIC_MAX_HOPS", 3)
	svc.EnableAgentic(
		application.AgenticConfig{MaxHops: maxHops, GraphTopN: config.GetInt("RAG_AGENTIC_GRAPH_TOPN", 5)},
		aiadapters.NewReasoner(reasonerGen, config.GetInt("RAG_REASONER_MAX_TOKENS", 1024)),
		graphExp,
	)
	log.Info().Bool("graph", graphExp != nil).Int("max_hops", maxHops).
		Str("reasoner_model", config.Get("RAG_REASONER_MODEL", config.Get("VLLM_MODEL", ""))).
		Msg("agentic controller enabled")
	return cleanup
}

// Кэш ответов в готовность не входит: конвейер переживает его отказ.
func readiness(qdrant *vectorstore.Qdrant, opensearch *searchstore.OpenSearch) observability.ReadyFunc {
	return func(ctx context.Context) error {
		if err := qdrant.Ping(ctx); err != nil {
			return err
		}
		return opensearch.Ping(ctx)
	}
}

func configureCacheRevision(svc *application.RAGService, ovr *runtimecfg.Overrides) {
	svc.SetCacheRevision(func(ctx context.Context) string {
		values := make([]string, 0, 10)
		for _, key := range []string{"VLLM_URL", "VLLM_MODEL", "VLLM_REASONING_EFFORT", "RAG_REASONER_MODEL",
			"EMBEDDINGS_URL", "EMBEDDINGS_MODEL", "EMBEDDING_DIM", "RERANKER_URL", "RERANKER_MODEL",
			"RAG_PIPELINE_REVISION", "RAG_SETTINGS_REVISION"} {
			values = append(values, ovr.Get(ctx, key, ""))
		}
		return strings.Join(values, "\x00")
	})
}
