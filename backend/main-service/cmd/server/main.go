package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/infrastructure/collections"
	"github.com/example/main-service/internal/infrastructure/kgstore"
	"github.com/example/main-service/internal/infrastructure/matproj"
	"github.com/example/main-service/internal/infrastructure/pubsearch"
	"github.com/example/main-service/internal/infrastructure/radarcollector"
	"github.com/example/main-service/internal/infrastructure/scoringstore"
	"github.com/example/main-service/internal/infrastructure/settingsstore"
	"github.com/example/main-service/internal/infrastructure/trendanalysis"
	"github.com/example/main-service/internal/infrastructure/trendsemantic"
	wshub "github.com/example/main-service/internal/infrastructure/ws"
	"github.com/example/main-service/internal/interfaces/httpapi"
	wsapi "github.com/example/main-service/internal/interfaces/ws"
	"github.com/example/main-service/internal/platform/aiclients"
	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/logger"
	"github.com/example/main-service/internal/platform/messaging"
	"github.com/example/main-service/internal/platform/observability"
	"github.com/example/main-service/internal/platform/ragclient"
	"github.com/example/main-service/internal/platform/runtimecfg"
	platformstorage "github.com/example/main-service/internal/platform/storage"
	"github.com/example/main-service/internal/platform/valkey"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(config.Get("METRICS_ADDR", ":9090")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	log := logger.New("main-service", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("main-service stopped with error")
		os.Exit(1)
	}
}

func healthcheck(metricsAddr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL(metricsAddr), nil)
	if err != nil {
		return fmt.Errorf("build healthcheck request: %w", err)
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

// jwtManagerFromEnv только проверяет токены (выпускает их auth-service); TTL читается лишь ради
// единой модели env платформы.
func jwtManagerFromEnv() *jwt.Manager {
	// Дефолт из compose при пустом JWT_SECRET принимал бы поддельные админские токены.
	secret := config.MustGet("JWT_SECRET")
	if secret == "dev-secret-change-me" {
		_, _ = fmt.Fprintln(os.Stderr,
			"FATAL: JWT_SECRET is the insecure default 'dev-secret-change-me'; set a strong secret in infra/.env")
		os.Exit(1)
	}
	return jwt.NewManager(
		secret,
		config.Get("JWT_ISSUER", "rag-platform"),
		config.GetDuration("JWT_ACCESS_TTL", 30*time.Minute),
	)
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := platformstorage.New(ctx, platformstorage.ConfigFromEnv())
	if err != nil {
		return err
	}

	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	llm, err := ragclient.New(config.MustGet("LLM_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = llm.Close() }()

	conn, err := messaging.Dial(ctx, config.MustGet("RABBITMQ_URL"), log)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if terr := conn.DeclareTopology(); terr != nil {
		return terr
	}

	jwtMgr := jwtManagerFromEnv()
	jwtMgr.SetSessionValidator(sessionValidator(db))

	vk, err := valkey.New(ctx, config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""), 0)
	if err != nil {
		return err
	}
	defer func() { _ = vk.Close() }()
	ovr := runtimecfg.New(vk)

	ingestion := application.NewIngestionService(store, db, llm)
	ingestion.EnableSourceReads(db, config.GetBool("RAG_SHARED_CORPUS", true), config.GetBool("SOURCE_INDEX_SNAPSHOTS", true))
	chat := sourceChatService(db, llm)
	admin := application.NewAdminService(db)
	hyp := buildHypothesisService(db, llm, vk, ingestion, ovr)

	uploads := application.NewUploadSessionService(
		store, store, vk, ingestion,
		int64(config.GetInt("UPLOAD_MAX_GB", 1))<<30,
	)

	// Обрыв AMQP роняет весь процесс: издатель живёт на том же соединении, и полуживой сервис молча
	// терял бы загрузки. Docker перезапустит его с новым соединением.
	ctx, failFatal := context.WithCancelCause(ctx)
	defer failFatal(nil)
	hub := wshub.NewHub(log)
	hub.SetOnIndexed(corpusEpochBumper(vk))
	go func() {
		if cerr := conn.ConsumeEvents(ctx, hub.HandleEvent); cerr != nil && ctx.Err() == nil {
			log.Error().Err(cerr).Msg("events consumer stopped; shutting down to reconnect")
			failFatal(cerr)
		}
	}()

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, readiness(db, store, llm), log)
	}()

	hypJobs := application.NewHypothesisJobService(hyp, db)
	stopHypJobs := hypJobs.StartWorker(ctx)
	defer stopHypJobs()
	usageSvc := application.NewLLMUsageService(vk, db)
	startUsageFlusher(ctx, usageSvc)
	api := httpapi.New(
		ingestion, chat, admin, uploads, hyp, hypJobs, vk, usageSvc, metrics,
		int64(config.GetInt("MAX_UPLOAD_MB", 50)),
		config.GetBool("RAG_SHARED_CORPUS", true),
	)
	analyses, err := analysisService(db, store, ovr)
	if err != nil {
		return err
	}
	api.SetAnalyses(analyses)
	if err = attachBackends(api, conn); err != nil {
		return err
	}
	setupRuntimeSettings(ctx, api, db, vk, ovr, log)
	collector := radarcollector.New(config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""))
	defer func() { _ = collector.Close() }()
	gateway, configured := liveGateway(ovr)
	live := startTrendLive(ctx, store, ovr, collector, db, llm, metrics, gateway, configured)
	api.SetTrendLive(live)
	api.SetLiveWatches(startLiveWatches(ctx, live, vk, db))
	setBriefs(api, gateway, configured)
	handler := apiHandler(api, hub, jwtMgr, vk, log)

	srv := httpx.NewServer(config.Get("HTTP_ADDR", ":8080"), handler, log)
	err = srv.Run(ctx)
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return fmt.Errorf("events consumer failed: %w", cause)
	}
	return err
}

func apiHandler(
	api *httpapi.API,
	hub *wshub.Hub,
	jwtMgr *jwt.Manager,
	tickets wsapi.TicketStore,
	log zerolog.Logger,
) http.Handler {
	protected := http.NewServeMux()
	api.Routes(protected)

	root := http.NewServeMux()
	websocket := wsapi.New(hub, jwtMgr, tickets, strings.Split(config.Get("WS_ALLOWED_ORIGINS", ""), ",")...)
	root.Handle("GET /ws", websocket)
	protected.HandleFunc("POST /ws-ticket", websocket.IssueTicket)
	root.Handle("/", jwtMgr.Middleware(httpapi.RestrictReadOnly(httpapi.RestrictAutomation(api.ModelDisplayNames(protected)))))

	return httpx.Chain(root,
		httpx.RequestID,
		httpx.Recover(log),
		httpx.LogRequests(log),
	)
}

func setBriefs(api *httpapi.API, gateway aiclients.Generator, configured func(context.Context) bool) {
	api.SetTechnologyBrief(application.NewTechnologyBriefWriter(gateway, configured))
	api.SetSignalBrief(application.NewLiveSignalBriefWriter(gateway, configured))
}

// liveGateway — шлюз LLM с журналом вызовов радара; один на все черновики, потому что настройки
// шлюза читаются на каждом вызове.
func liveGateway(ovr *runtimecfg.Overrides) (aiclients.Generator, func(context.Context) bool) {
	gateway := application.NewLiveAuditedGenerator(
		aiclients.NewDynamicGenerator(application.LLMTarget(ovr.Get), nil),
		func(ctx context.Context) string { return ovr.Get(ctx, "VLLM_MODEL", "") })
	return gateway, func(ctx context.Context) bool { return ovr.Get(ctx, "VLLM_URL", "") != "" }
}

func startTrendLive(
	ctx context.Context, store *platformstorage.Client, ovr *runtimecfg.Overrides,
	collector *radarcollector.Coordinator, db *dbclient.Client, llm *ragclient.Client,
	metrics *observability.Metrics, gateway aiclients.Generator, configured func(context.Context) bool,
) *application.TrendLiveService {
	live := application.NewTrendLiveService(store, func(ctx context.Context) string {
		return strings.TrimSpace(ovr.Get(ctx, "GITHUB_TOKEN", ""))
	})
	live.SetWriterGuard(collector.Acquire)
	live.SetStageRecorder(metrics.RecordStage)
	// Сырые записи поиска в базе переживают перезапуск и переиспользуются близким запросом.
	live.SetSearchStore(db)
	live.SetCollectionReader(collector.Status)
	live.SetCorpusSearch(llm, db)
	live.SetSemanticEncoder(trendsemantic.New(
		config.Get("EMBEDDINGS_URL", "http://f2llm-service:8085/v1/embeddings"),
		config.Get("EMBEDDINGS_MODEL", "codefuse-ai/F2LLM-v2-0.6B"),
	))
	live.SetAnalyzer(trendanalysis.New(config.Get("RADAR_ANALYSIS_URL", "http://radar-analysis:8080")))
	// API-реплика сама отвечает на открытый запрос, пока сборщик занят циклом, поэтому
	// перевод запроса и русское название технологии нужны и здесь.
	live.SetPlanner(application.NewLiveQueryPlanner(gateway, configured))
	live.SetNamer(application.NewTopicNamer(gateway, configured))
	live.SetDescriber(application.NewTopicDescriber(gateway, configured))
	live.SetQualifier(application.NewCandidateQualifier(gateway, configured))
	live.SetExtractor(application.NewCandidateExtractor(gateway, configured))
	live.SetCardWriter(application.NewCandidateCard(gateway, configured))
	live.SetEventCheck(application.NewLiveEventCheck(gateway, configured))
	live.SetExecutiveWriter(application.NewLiveExecutiveWriter(gateway, configured))
	live.SetSourceDigest(application.NewSourceDigest(gateway, configured))
	live.SetTechnologyPhraser(application.NewTechnologyPhraser(gateway, configured))
	live.SetMaturityRater(application.NewLiveMaturityRater(gateway, configured))
	live.SetWorkers(ovr.GetInt)
	// Сервис ML-команды приоритетен; без его адреса работает встроенная модель этапа 1.
	if classifier := application.NewCandidateClassifier(config.Get("RADAR_CLASSIFIER_URL", "")); classifier != nil {
		live.SetClassifier(classifier, config.Get("RADAR_CLASSIFIER_VERSION", ""))
	} else if err := live.UseEmbeddedClassifier(); err != nil {
		slog.Error("radar embedded classifier", "error", err)
	}
	live.SetMLCandidateSource(config.Get("RADAR_CANDIDATE_SOURCE", "ngrams"),
		application.NewMLCandidateExtractor(config.Get("RADAR_ML_EXTRACT_URL", ""),
			config.GetDuration("RADAR_ML_EXTRACT_TIMEOUT", 300*time.Second),
			config.GetInt("RADAR_ML_EXTRACT_MAX_REQUEST_BYTES", 4<<20), config.GetInt("RADAR_ML_EXTRACT_MAX_RESPONSE_BYTES", 1<<20)),
		config.Get("RADAR_ML_EXTRACT_VERSION", config.Get("RADAR_CLASSIFIER_VERSION", "")))
	if config.GetBool("RADAR_EXTERNAL_COLLECTOR", false) {
		live.StartReader(ctx)
	} else {
		live.Start(ctx)
	}
	return live
}

func startLiveWatches(ctx context.Context, live *application.TrendLiveService, vk *valkey.Client,
	db *dbclient.Client,
) *application.LiveWatches {
	watches := application.NewLiveWatches(live, vk, func(ctx context.Context) ([]string, error) {
		users, err := db.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(users))
		for _, user := range users {
			ids = append(ids, user.GetId())
		}
		return ids, nil
	})
	// Прогнозы из сигналов сверяет тот же цикл: он уже знает, какие ответы пересобраны.
	watches.SetBets(db)
	watches.Start(ctx)
	return watches
}

// buildHypothesisService без MP_API_KEY подставляет безопасную заглушку Materials Project.
func buildHypothesisService(
	db *dbclient.Client, llm *ragclient.Client, vk *valkey.Client,
	ingestion *application.IngestionService, ovr *runtimecfg.Overrides,
) *application.HypothesisService {
	hyp := application.NewHypothesisService(
		db, llm, scoringstore.New(vk), settingsstore.New(vk),
		matproj.New(config.Get("MP_API_KEY", "")), kgstore.New(vk), llm,
	)
	hyp.SetPubSearcher(pubsearch.NewAdapter(ovr))
	hyp.SetPubIngestor(ingestion)
	hyp.SetRuntimeOverrides(ovr)
	return hyp
}

func setupRuntimeSettings(
	ctx context.Context, api *httpapi.API, db *dbclient.Client, vk *valkey.Client,
	ovr *runtimecfg.Overrides, log zerolog.Logger,
) {
	s := httpapi.NewAppSettingsStore(db, vk)
	api.SetAppSettingsStore(s)
	api.SetRuntimeOverrides(ovr)
	startSettingsPublisher(ctx, s, log)
}

// startSettingsPublisher зеркалит оверрайды из БД в Valkey раз в минуту, чтобы они пережили сброс Valkey.
func startSettingsPublisher(ctx context.Context, s *httpapi.AppSettingsStore, log zerolog.Logger) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			if err := s.Republish(ctx); err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Msg("app settings republish failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// startUsageFlusher переносит дневные счётчики LLM из Valkey в Postgres, чтобы история пережила вытеснение.
func startUsageFlusher(ctx context.Context, usage *application.LLMUsageService) {
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				usage.Flush(ctx)
			}
		}
	}()
}

// corpusEpochBumper поднимает эпоху корпуса кэша ответов при индексации документа, списывая устаревшие ответы.
func corpusEpochBumper(vk *valkey.Client) func(context.Context, string) {
	const epochTTL = 30 * 24 * time.Hour
	return func(ctx context.Context, ownerID string) {
		_, _ = vk.IncrTTL(ctx, "rag:corpus_epoch:shared", epochTTL)
		if ownerID != "" {
			_, _ = vk.IncrTTL(ctx, "rag:corpus_epoch:"+ownerID, epochTTL)
		}
	}
}

func readiness(db *dbclient.Client, store *platformstorage.Client, llm *ragclient.Client) observability.ReadyFunc {
	return func(ctx context.Context) error {
		if err := db.Ping(ctx); err != nil {
			return err
		}
		if err := store.Ping(ctx); err != nil {
			return err
		}
		return llm.Ping(ctx)
	}
}

func sourceChatService(db *dbclient.Client, llm *ragclient.Client) *application.ChatService {
	chat := application.NewChatService(db, llm)
	chat.EnableSourceReads(db, config.GetBool("RAG_SHARED_CORPUS", true))
	return chat
}

func attachBackends(api *httpapi.API, conn *messaging.Conn) error {
	addr := config.Get("DB_SERVICE_ADDR", "db-service:9091")
	client, err := collections.New(addr)
	if err != nil {
		return err
	}
	api.Collections(client)
	technologies, err := collections.NewTechnologies(addr)
	if err != nil {
		return err
	}
	api.SetTechnologies(technologies)
	api.SetReviewQueue(conn)
	return nil
}

func analysisService(db application.AnalysisJobs, store *platformstorage.Client, ovr *runtimecfg.Overrides,
) (*application.AnalysisService, error) {
	analyses, err := application.NewAnalysisService(db, store, analysisdoc.DevelopmentMethod)
	if err != nil {
		return nil, err
	}
	analyses.SetTranslator(application.NewQueryTranslator(
		aiclients.NewDynamicGenerator(application.LLMTarget(ovr.Get), nil),
		func(ctx context.Context) bool { return ovr.Get(ctx, "VLLM_URL", "") != "" }, analysisdoc.MaxSearchTerms))
	return analyses, nil
}
