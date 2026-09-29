package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	"github.com/example/db-service/internal/application"
	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/interfaces/grpcserver"
	"github.com/example/db-service/internal/platform/config"
	"github.com/example/db-service/internal/platform/grpcx"
	"github.com/example/db-service/internal/platform/logger"
	"github.com/example/db-service/internal/platform/observability"
	"github.com/example/db-service/internal/platform/storage"

	analysisv1 "github.com/example/db-service/internal/platform/genproto/analysis/v1"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
	harvestv1 "github.com/example/db-service/internal/platform/genproto/harvest/v1"
	indexdocv1 "github.com/example/db-service/internal/platform/genproto/indexdoc/v1"
	technologyv1 "github.com/example/db-service/internal/platform/genproto/technology/v1"
)

func main() {
	log := logger.New("db-service", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("db-service stopped with error")
		os.Exit(1)
	}
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Connect(ctx, config.MustGet("POSTGRES_DSN"), log)
	if err != nil {
		return err
	}
	defer db.Close()
	if merr := db.Migrate(ctx); merr != nil {
		return merr
	}

	documents := postgres.NewDocumentRepo(db)
	go dispatchIngestion(ctx, documents, log)
	svc := application.New(
		postgres.NewUserRepo(db),
		documents,
		postgres.NewChatRepo(db),
		postgres.NewModelRepo(db),
		postgres.NewKPIRepo(db).WithSharedCorpus(config.GetBool("RAG_SHARED_CORPUS", true)),
		postgres.NewClusterRepo(db),
		postgres.NewHypothesisRepo(db),
		postgres.NewLLMUsageRepo(db),
		postgres.NewAppSettingsRepo(db),
	)
	if serr := seed(ctx, svc); serr != nil {
		return serr
	}

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, db.Ping, log)
	}()

	srv := grpcx.NewServer(log)
	objects, err := storage.Open(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}
	dbv1.RegisterDbServiceServer(srv.Registrar(), grpcserver.New(svc))
	dbv1.RegisterDocumentQueriesServer(
		srv.Registrar(),
		grpcserver.NewDocumentQueries(
			postgres.NewDocumentQueries(
				db,
				config.GetBool(
					"RAG_SHARED_CORPUS",
					true,
				),
			),
		),
	)
	dbv1.RegisterHypothesisJobsServer(srv.Registrar(), grpcserver.NewHypothesisJobs(postgres.NewHypothesisJobsRepo(db)))
	dbv1.RegisterChatTurnsServer(srv.Registrar(), grpcserver.NewChatTurns(postgres.NewChatRepo(db)))
	dbv1.RegisterIngestionControlServer(srv.Registrar(), grpcserver.NewIngestion(documents))
	dbv1.RegisterAuthSessionsServer(srv.Registrar(), grpcserver.NewAuthSessions(postgres.NewAuthSessionsRepo(db)))
	technologyv1.RegisterTechnologyServiceServer(srv.Registrar(), grpcserver.NewTechnology(
		postgres.NewTechnologyRepo(db).WithSharedCorpus(config.GetBool("RAG_SHARED_CORPUS", true))))
	harvestv1.RegisterHarvestServiceServer(srv.Registrar(),
		grpcserver.NewHarvest(postgres.NewHarvestRepo(db).WithPanel(collectionPanel())))
	indexdocv1.RegisterIndexServiceServer(srv.Registrar(), grpcserver.NewIndex(postgres.NewIndexRepo(db)))
	analysisv1.RegisterAnalysisServiceServer(srv.Registrar(), grpcserver.NewAnalysis(postgres.NewAnalysisJobs(db, objects)))
	return srv.Run(ctx, config.Get("GRPC_ADDR", ":9091"))
}

func seed(ctx context.Context, svc *application.Service) error {
	adminPass := config.MustGet("ADMIN_PASSWORD")
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	models := []*domain.Model{
		{ID: "qwen3-35b-a3b", Name: "Qwen3-35B-A3B", Role: "generation", Backend: "vLLM"},
		{ID: "f2llm-v2-0.6b", Name: "F2LLM-v2-0.6B", Role: "embeddings", Backend: "llama.cpp"},
		{ID: "qwen3-reranker-0.6b", Name: "Qwen3-Reranker-0.6B", Role: "reranker", Backend: "llama.cpp"},
		{ID: "paddleocr-vl", Name: "PaddleOCR-VL", Role: "ocr", Backend: "paddleocr-vl-service"},
		{ID: "qwen3.5-9b", Name: "Qwen3.5-9B", Role: "vlm", Backend: "VLM Service"},
	}
	if err = svc.Seed(ctx, config.Get("ADMIN_USERNAME", "admin"), string(hash), models); err != nil {
		return err
	}
	automationPassword := config.Get("AUTOMATION_PASSWORD", "")
	if automationPassword == "" && config.Get("APP_ENV", "") != "production" {
		return nil
	}
	if len(automationPassword) < 32 {
		return errors.New("AUTOMATION_PASSWORD must contain at least 32 bytes")
	}
	automationHash, err := bcrypt.GenerateFromPassword([]byte(automationPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	return svc.SeedAutomation(ctx, config.Get("AUTOMATION_USERNAME", "automation"), string(automationHash))
}

// collectionPanel берётся из конфигурации, а не из запроса, чтобы вызывающий не мог его расширить.
func collectionPanel() []string {
	panel := []string{}
	for _, domain := range strings.Split(config.Get("COLLECTION_ALLOWED_DOMAINS", ""), ",") {
		if domain = strings.TrimSpace(domain); domain != "" {
			panel = append(panel, domain)
		}
	}
	return panel
}
