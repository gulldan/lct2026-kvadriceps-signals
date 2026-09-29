package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/ocr-service/internal/application"
	ocrinfra "github.com/example/ocr-service/internal/infrastructure/ocr"
	amqpiface "github.com/example/ocr-service/internal/interfaces/amqp"
	"github.com/example/ocr-service/internal/platform/aiclients"
	"github.com/example/ocr-service/internal/platform/config"
	"github.com/example/ocr-service/internal/platform/contracts"
	"github.com/example/ocr-service/internal/platform/dbclient"
	"github.com/example/ocr-service/internal/platform/logger"
	"github.com/example/ocr-service/internal/platform/messaging"
	"github.com/example/ocr-service/internal/platform/observability"
	"github.com/example/ocr-service/internal/platform/storage"
)

func main() {
	log := logger.New("ocr-service", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("ocr-service stopped with error")
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

	// Пустой OCR_ENGINE_URL даёт детерминированную заглушку: приём документов работает без нейросети.
	ocrClient := aiclients.NewOCR(config.Get("OCR_ENGINE_URL", ""), config.Get("OCR_MODEL", ""),
		&http.Client{Timeout: config.GetDuration("OCR_HTTP_TIMEOUT", 20*time.Minute)})

	processor := application.New(ocrinfra.NewExtractor(ocrClient), store, db, messaging.Durable{Events: pub, Outbox: db})

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, db.Ping, log)
	}()

	cfg := messaging.ConsumerConfig{
		Queue:         contracts.RouteParseOCR,
		Workers:       config.GetInt("WORKER_CONCURRENCY", 4),
		Prefetch:      0,
		HandleTimeout: config.GetDuration("OCR_HANDLE_TIMEOUT", 30*time.Minute),
		Recorder:      metrics,
	}
	return conn.Consume(ctx, cfg, amqpiface.Handler(processor))
}
