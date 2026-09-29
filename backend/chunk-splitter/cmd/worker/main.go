package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/chunk-splitter/internal/application"
	"github.com/example/chunk-splitter/internal/infrastructure/embedding"
	"github.com/example/chunk-splitter/internal/infrastructure/lexicalindex"
	"github.com/example/chunk-splitter/internal/infrastructure/pacing"
	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	"github.com/example/chunk-splitter/internal/infrastructure/titler"
	"github.com/example/chunk-splitter/internal/infrastructure/tokenizer"
	"github.com/example/chunk-splitter/internal/infrastructure/vectorindex"
	amqpiface "github.com/example/chunk-splitter/internal/interfaces/amqp"
	"github.com/example/chunk-splitter/internal/platform/aiclients"
	"github.com/example/chunk-splitter/internal/platform/config"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	"github.com/example/chunk-splitter/internal/platform/dbclient"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/llmusage"
	"github.com/example/chunk-splitter/internal/platform/logger"
	"github.com/example/chunk-splitter/internal/platform/messaging"
	"github.com/example/chunk-splitter/internal/platform/observability"
	"github.com/example/chunk-splitter/internal/platform/searchstore"
	"github.com/example/chunk-splitter/internal/platform/storage"
	"github.com/example/chunk-splitter/internal/platform/valkey"
	"github.com/example/chunk-splitter/internal/platform/vectorstore"
)

func main() {
	log := logger.New("chunk-splitter", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("chunk-splitter stopped with error")
		os.Exit(1)
	}
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Размерность обязана совпадать и с эмбеддером, и с коллекцией Qdrant.
	embeddingDim := config.GetInt("EMBEDDING_DIM", 1024)

	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Коллекцией владеет chunk-splitter; llm-service её только читает.
	qdrant := vectorstore.NewQdrant(config.MustGet("QDRANT_URL"), config.Get("QDRANT_COLLECTION", "documents"))
	if cerr := qdrant.EnsureCollection(ctx, embeddingDim); cerr != nil {
		return fmt.Errorf("ensure qdrant collection: %w", cerr)
	}

	opensearch := searchstore.NewOpenSearch(config.MustGet("OPENSEARCH_URL"), config.Get("OPENSEARCH_INDEX", "chunks"))
	if ierr := opensearch.EnsureIndex(ctx); ierr != nil {
		return fmt.Errorf("ensure opensearch index: %w", ierr)
	}

	// Без EMBEDDINGS_URL эмбеддер — детерминированная заглушка, и конвейер работает без GPU.
	embedder := aiclients.NewEmbedder(
		config.Get("EMBEDDINGS_URL", ""),
		config.Get("EMBEDDINGS_MODEL", ""),
		embeddingDim,
		nil,
	)

	conn, err := messaging.Dial(ctx, config.MustGet("RABBITMQ_URL"), log)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if terr := conn.DeclareTopology(); terr != nil {
		return terr
	}
	pub, err := conn.NewPublisher()
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()

	// Темп уступает пользовательским запросам, а при недоступном Valkey отключается:
	// сигнал нагрузки — оптимизация, а не зависимость.
	var pacer application.Pacer
	var usageKV llmusage.KV // nil, если Valkey недоступен
	if vk, verr := valkey.New(ctx, config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""), 0); verr != nil {
		log.Warn().Err(verr).Msg("valkey unavailable; ingestion pacing disabled")
	} else {
		defer func() { _ = vk.Close() }()
		pacer = pacing.New(vk, config.GetDuration("INGEST_YIELD_MAX_WAIT", 2*time.Second))
		usageKV = vk
	}

	store, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}

	// Посимвольный сплиттер нужен всегда: он запасной, когда токенизатор не настроен.
	overlap := config.GetInt("CHUNK_OVERLAP", 150)
	chunker := splitter.NewRecursive(config.GetInt("CHUNK_SIZE", 1000), overlap)

	// Без клиента tok остаётся nil-интерфейсом: типизированный nil-указатель обманул бы проверку на nil в индексаторе.
	var tok application.Tokenizer
	if lc := tokenizer.New(config.Get("TOKENIZER_URL", ""), nil); lc != nil {
		tok = lc
	}

	metrics := observability.NewMetrics()
	indexer := application.New(
		chunker,
		db,
		embedding.New(sourceEmbedder(embedder)),
		vectorindex.New(qdrant),
		lexicalindex.New(opensearch),
		messaging.Durable{Events: pub, Outbox: db},
		pacer,
		store,
		tok,
		config.GetInt("CHUNK_MAX_TOKENS", 512),
		config.GetInt("TEXT_MAX_MB", 16)<<20,
		config.GetInt("EMBED_BATCH", 64),
		config.GetInt("SPLIT_WINDOW_MB", 8)<<20,
		overlap,
		config.GetBool("CONTEXTUAL_HEADERS", true),
		metrics,
	)

	configureSourceIndexes(indexer, db, store, embeddingDim)
	configureEnrichers(indexer, db, usageKV)

	ready := readiness(db, qdrant, opensearch)
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, ready, log)
	}()

	cfg := messaging.ConsumerConfig{
		Queue:    contracts.RouteChunk,
		Workers:  config.GetInt("WORKER_CONCURRENCY", 2),
		Prefetch: 0,
		// Зависший эмбеддер или хранилище не должны держать слот воркера; запас большой — крупные
		// документы индексируются долго.
		HandleTimeout: config.GetDuration("CHUNK_HANDLE_TIMEOUT", 30*time.Minute),
		Recorder:      metrics,
	}
	return conn.Consume(ctx, cfg, amqpiface.Handler(indexer))
}

func configureEnrichers(ix *application.Indexer, db *dbclient.Client, usageKV llmusage.KV) {
	configureTitles(ix, db, usageKV)
	ix.EnableKinds(db)
	ix.EnableMeta(db)
}

func configureTitles(ix *application.Indexer, setter application.TitleSetter, usageKV llmusage.KV) {
	if config.Get("VLLM_URL", "") == "" {
		return
	}
	ix.EnableTitles(titler.New(titler.Generator(), usageKV), setter)
}

func readiness(db *dbclient.Client, qdrant *vectorstore.Qdrant, opensearch *searchstore.OpenSearch) observability.ReadyFunc {
	return func(ctx context.Context) error {
		if err := db.Ping(ctx); err != nil {
			return err
		}
		if err := qdrant.Ping(ctx); err != nil {
			return err
		}
		return opensearch.Ping(ctx)
	}
}

func configureSourceIndexes(ix *application.Indexer, db *dbclient.Client, store *storage.Client, embeddingDim int) {
	if config.GetBool("SOURCE_INDEX_SNAPSHOTS", true) {
		ix.EnableSourceIndexes(db, store, indexdoc.Model{
			Name: config.MustGet("EMBEDDINGS_MODEL"), Revision: config.MustGet("EMBEDDINGS_REVISION"), Dimension: embeddingDim,
		})
	}
}

func sourceEmbedder(legacy aiclients.Embedder) aiclients.Embedder {
	if !config.GetBool("SOURCE_INDEX_SNAPSHOTS", true) {
		return legacy
	}
	return aiclients.NewVersionedEmbedder(config.MustGet("EMBEDDINGS_URL"), config.MustGet("EMBEDDINGS_MODEL"),
		config.MustGet("EMBEDDINGS_REVISION"), legacy.Dim(), nil)
}
