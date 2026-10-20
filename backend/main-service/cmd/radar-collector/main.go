// radar-collector владеет живым снимком; API-реплики только читают его результат.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/infrastructure/radarcollector"
	"github.com/example/main-service/internal/infrastructure/trendanalysis"
	"github.com/example/main-service/internal/infrastructure/trendsemantic"
	"github.com/example/main-service/internal/platform/aiclients"
	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/runtimecfg"
	"github.com/example/main-service/internal/platform/storage"
	"github.com/example/main-service/internal/platform/valkey"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	once := flag.Bool("once", false, "collect the current window once and exit")
	analysisOnly := flag.Bool("analysis-only", false, "enrich the saved observation without recollecting science")
	sourcesOnly := flag.Bool("sources-only", false, "refresh patents and web once under the shared writer lease")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	objects, err := storage.Open(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}
	if (*sourcesOnly && *once) || (*analysisOnly && (*once || *sourcesOnly)) {
		return errors.New("choose one of --once, --sources-only or --analysis-only")
	}
	kv, err := valkey.New(ctx, config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""), 0)
	if err != nil {
		return err
	}
	defer func() { _ = kv.Close() }()
	settings := runtimecfg.New(kv)
	live := application.NewTrendLiveService(objects, func(ctx context.Context) string {
		return settings.Get(ctx, "GITHUB_TOKEN", "")
	})
	live.SetSemanticEncoder(trendsemantic.New(
		config.Get("EMBEDDINGS_URL", "http://f2llm-service:8085/v1/embeddings"),
		config.Get("EMBEDDINGS_MODEL", "codefuse-ai/F2LLM-v2-0.6B")))
	live.SetAnalyzer(trendanalysis.New(config.Get("RADAR_ANALYSIS_URL", "http://radar-analysis:8080")))
	// Разовый поиск отвечает отсюда, потому что окно arXiv лежит в памяти сборщика; без
	// настроенного шлюза запрос не переводится и поиск идёт по исходному тексту.
	gateway := application.NewLiveAuditedGenerator(
		aiclients.NewDynamicGenerator(application.LLMTarget(settings.Get), nil),
		func(ctx context.Context) string { return settings.Get(ctx, "VLLM_MODEL", "") }, kv)
	configured := func(ctx context.Context) bool { return settings.Get(ctx, "VLLM_URL", "") != "" }
	live.SetNamer(application.NewTopicNamer(gateway, configured))
	live.SetDescriber(application.NewTopicDescriber(gateway, configured))
	live.SetWorkers(settings.GetInt)
	live.SetPlanner(application.NewLiveQueryPlanner(gateway, configured))
	live.LoadWindow(ctx)
	go serveSearch(ctx, live, config.Get("RADAR_COLLECTOR_ADDR", ":8091"))
	coordinator := radarcollector.New(config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""))
	defer func() { _ = coordinator.Close() }()
	if *analysisOnly {
		live.SetWriterGuard(coordinator.Acquire)
		return live.RefreshAnalysis(application.LiveInBackground(ctx))
	}
	// Проходы сборщика никто не ждёт: модель фона и запас OpenAlex для живого поиска. serveSearch выше получает
	// исходный контекст — перевод запроса оператора идёт быстрым маршрутом.
	return radarcollector.Run(application.LiveInBackground(ctx), live, coordinator, *once, *sourcesOnly)
}

// serveAbstracts отдаёт аннотации работ окна по id: API-реплика дописывает по
// ним русские пересказы сохранённым ответам, а окна в её памяти нет.
func serveAbstracts(live *application.TrendLiveService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.UnmarshalRead(io.LimitReader(r.Body, 1<<20), &req); err != nil {
			http.Error(w, "invalid abstracts request", http.StatusBadRequest)
			return
		}
		abstracts, loaded := live.WindowAbstracts(req.IDs)
		if !loaded {
			http.Error(w, "window is not loaded yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(w, abstracts); err != nil {
			slog.Warn("radar abstracts response", "error", err)
		}
	}
}

// serveSearch — внутренний HTTP сборщика для API-реплики: здоровье и разовый поиск по окну.
// Наружу порт не публикуется.
func serveSearch(ctx context.Context, live *application.TrendLiveService, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /v1/abstracts", serveAbstracts(live))
	mux.HandleFunc("POST /v1/search", func(w http.ResponseWriter, r *http.Request) {
		var req application.LiveWindowSearch
		if err := json.UnmarshalRead(io.LimitReader(r.Body, 64<<10), &req); err != nil || len(req.Periods) == 0 {
			http.Error(w, "invalid search", http.StatusBadRequest)
			return
		}
		result, err := live.SearchWindow(r.Context(), req)
		switch {
		case errors.Is(err, application.ErrLiveWindowEmpty):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(w, result); err != nil {
			slog.Warn("radar search response", "error", err)
		}
	})
	// Признаки встроенной модели считаются здесь, у окна: наружу уходят шесть
	// чисел на фразу, а не пять тысяч работ с аннотациями на каждого кандидата.
	mux.HandleFunc("POST /v1/features", func(w http.ResponseWriter, r *http.Request) {
		var req application.LiveFeatureRequest
		if err := json.UnmarshalRead(io.LimitReader(r.Body, 64<<10), &req); err != nil || len(req.Periods) == 0 ||
			len(req.Phrases) == 0 || len(req.Phrases) > 64 {
			http.Error(w, "invalid features request", http.StatusBadRequest)
			return
		}
		features, err := live.WindowFeatures(r.Context(), req)
		switch {
		case errors.Is(err, application.ErrLiveWindowEmpty):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(w, features); err != nil {
			slog.Warn("radar features response", "error", err)
		}
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("radar search server", "error", err)
	}
}
