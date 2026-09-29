package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/email-parser/internal/application"
	emailinfra "github.com/example/email-parser/internal/infrastructure/email"
	amqpiface "github.com/example/email-parser/internal/interfaces/amqp"
	"github.com/example/email-parser/internal/platform/config"
	"github.com/example/email-parser/internal/platform/contracts"
	"github.com/example/email-parser/internal/platform/dbclient"
	"github.com/example/email-parser/internal/platform/logger"
	"github.com/example/email-parser/internal/platform/messaging"
	"github.com/example/email-parser/internal/platform/observability"
	"github.com/example/email-parser/internal/platform/storage"
)

func main() {
	log := logger.New("email-parser", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("email-parser stopped with error")
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

	// TIKA_URL нужен только для файлов Outlook .msg.
	processor := application.New(
		emailinfra.NewExtractor(
			config.Get(
				"TIKA_URL",
				"",
			),
		),
		store,
		db,
		messaging.Durable{
			Events: pub,
			Outbox: db,
		},
	)

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, db.Ping, log)
	}()

	cfg := messaging.ConsumerConfig{
		Queue:         contracts.RouteParseEmail,
		Workers:       config.GetInt("WORKER_CONCURRENCY", 4),
		Prefetch:      0,
		HandleTimeout: config.GetDuration("EMAIL_HANDLE_TIMEOUT", 5*time.Minute),
		Recorder:      metrics,
	}
	return conn.Consume(ctx, cfg, amqpiface.Handler(processor))
}
