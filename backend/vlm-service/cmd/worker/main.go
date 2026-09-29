package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/vlm-service/internal/application"
	vlminfra "github.com/example/vlm-service/internal/infrastructure/vlm"
	amqpiface "github.com/example/vlm-service/internal/interfaces/amqp"
	"github.com/example/vlm-service/internal/platform/config"
	"github.com/example/vlm-service/internal/platform/contracts"
	"github.com/example/vlm-service/internal/platform/dbclient"
	"github.com/example/vlm-service/internal/platform/logger"
	"github.com/example/vlm-service/internal/platform/messaging"
	"github.com/example/vlm-service/internal/platform/observability"
	"github.com/example/vlm-service/internal/platform/storage"
)

func main() {
	log := logger.New("vlm-service", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("vlm-service stopped with error")
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

	describer := vlminfra.NewDescriber(
		config.Get("VLM_ENGINE_URL", ""),
		config.Get("VLM_MODEL", ""),
		config.Get("VLM_API_KEY", ""),
		config.Get("VLM_API_STYLE", "native"),
	)

	processor := application.New(describer, store, db, messaging.Durable{Events: pub, Outbox: db})

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, db.Ping, log)
	}()

	cfg := messaging.ConsumerConfig{
		Queue:         contracts.RouteParseVLM,
		Workers:       config.GetInt("WORKER_CONCURRENCY", 4),
		Prefetch:      0,
		HandleTimeout: config.GetDuration("VLM_HANDLE_TIMEOUT", 30*time.Minute),
		Recorder:      metrics,
	}
	return conn.Consume(ctx, cfg, amqpiface.Handler(processor))
}
