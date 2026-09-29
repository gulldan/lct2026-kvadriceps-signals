package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/archive-worker/internal/application"
	"github.com/example/archive-worker/internal/infrastructure/archivescan"
	amqpiface "github.com/example/archive-worker/internal/interfaces/amqp"
	"github.com/example/archive-worker/internal/platform/config"
	"github.com/example/archive-worker/internal/platform/contracts"
	"github.com/example/archive-worker/internal/platform/dbclient"
	"github.com/example/archive-worker/internal/platform/logger"
	"github.com/example/archive-worker/internal/platform/messaging"
	"github.com/example/archive-worker/internal/platform/observability"
	"github.com/example/archive-worker/internal/platform/storage"
)

const mib int64 = 1 << 20

func main() {
	log := logger.New("archive-worker", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("archive-worker stopped with error")
		os.Exit(1)
	}
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}
	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

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

	extractor := archivescan.New(
		config.MustGet("ARCHIVE_SCAN_URL"),
		store,
		config.GetDuration("ARCHIVE_HTTP_TIMEOUT", 2*time.Hour),
		archivescan.Limits{
			MaxFileBytes:   int64(config.GetInt("ARCHIVE_MAX_FILE_MB", 64)) * mib,
			MaxTotalBytes:  int64(config.GetInt("ARCHIVE_MAX_TOTAL_MB", 1024)) * mib,
			MaxRatio:       config.GetFloat("ARCHIVE_MAX_RATIO", 200),
			ExtractTimeout: time.Duration(config.GetInt("ARCHIVE_EXTRACT_TIMEOUT", 3600)) * time.Second,
		},
	)
	processor := application.New(extractor, db, pub, config.GetInt("ARCHIVE_MAX_ENTRIES", 2000))

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, db.Ping, log)
	}()

	cfg := messaging.ConsumerConfig{
		Queue:    contracts.RouteParseArch,
		Workers:  config.GetInt("WORKER_CONCURRENCY", 4),
		Prefetch: 0,
		// Должен заметно превышать ARCHIVE_HTTP_TIMEOUT плюс регистрацию всех записей,
		// иначе долгая распаковка оборвётся и уйдёт в dead-letter.
		HandleTimeout: config.GetDuration("ARCHIVE_HANDLE_TIMEOUT", 3*time.Hour),
		Recorder:      metrics,
	}
	return conn.Consume(ctx, cfg, amqpiface.Handler(processor))
}
