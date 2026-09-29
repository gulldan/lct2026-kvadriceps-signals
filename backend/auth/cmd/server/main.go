package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/auth/internal/application"
	"github.com/example/auth/internal/infrastructure/directory"
	"github.com/example/auth/internal/infrastructure/sessions"
	"github.com/example/auth/internal/infrastructure/throttle"
	"github.com/example/auth/internal/interfaces/httpapi"
	"github.com/example/auth/internal/platform/config"
	"github.com/example/auth/internal/platform/dbclient"
	"github.com/example/auth/internal/platform/httpx"
	"github.com/example/auth/internal/platform/jwt"
	"github.com/example/auth/internal/platform/logger"
	"github.com/example/auth/internal/platform/observability"
	"github.com/example/auth/internal/platform/valkey"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(config.Get("METRICS_ADDR", ":9090")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	log := logger.New("auth-service", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("auth-service stopped with error")
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

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Dev-значение по умолчанию compose подставляет при пустом JWT_SECRET — с ним токены подделываются.
	jwtSecret := config.MustGet("JWT_SECRET")
	if jwtSecret == "dev-secret-change-me" {
		return errors.New("JWT_SECRET is the insecure default 'dev-secret-change-me'; set a strong secret in infra/.env")
	}
	manager := jwt.NewManager(
		jwtSecret,
		config.Get("JWT_ISSUER", "rag-platform"),
		config.GetDuration("JWT_ACCESS_TTL", 30*time.Minute),
	)

	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	vk, err := valkey.New(ctx, config.Get("VALKEY_ADDR", "valkey:6379"), config.Get("VALKEY_PASSWORD", ""), 0)
	if err != nil {
		return err
	}
	defer func() { _ = vk.Close() }()

	loginThrottle := throttle.New(vk, throttle.Config{
		MaxAttempts: config.GetInt("AUTH_LOGIN_MAX_ATTEMPTS", 10),
		Window:      time.Duration(config.GetInt("AUTH_LOGIN_WINDOW_SEC", 900)) * time.Second,
		Lockout:     time.Duration(config.GetInt("AUTH_LOGIN_LOCKOUT_SEC", 900)) * time.Second,
	})

	dbTimeout := time.Duration(config.GetInt("AUTH_DB_TIMEOUT_SEC", 5)) * time.Second

	svc := application.New(
		directory.New(db, dbTimeout),
		sessions.New(db, config.GetDuration("REFRESH_TTL", 720*time.Hour)),
		manager,
		loginThrottle,
	)

	metrics := observability.NewMetrics()
	go func() {
		_ = observability.RunOps(ctx, config.Get("METRICS_ADDR", ":9090"), metrics, readiness(db, vk), log)
	}()

	mux := http.NewServeMux()
	httpapi.New(svc).Routes(mux)

	handler := httpx.Chain(gateRegistration(manager, mux),
		httpx.RequestID,
		httpx.Recover(log),
		httpx.LogRequests(log),
	)

	srv := httpx.NewServer(config.Get("HTTP_ADDR", ":8080"), handler, log)
	return srv.Run(ctx)
}

// gateRegistration пускает анонимный POST /register только при REGISTRATION_ADMIN_ONLY=false:
// из интернета не зарегистрироваться, а админка проходит с токеном администратора и одна
// может назначить роли новой учётке.
func gateRegistration(manager *jwt.Manager, next http.Handler) http.Handler {
	adminOnly := config.GetBool("REGISTRATION_ADMIN_ONLY", true)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/register" {
			if callerIsAdmin(manager, r) {
				r = httpapi.ByAdmin(r)
			} else if adminOnly {
				http.Error(w, `{"error":"registration is disabled on this deployment"}`, http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func callerIsAdmin(manager *jwt.Manager, r *http.Request) bool {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return false
	}
	claims, err := manager.Parse(parts[1])
	if err != nil {
		return false
	}
	// Демо-админ с ролью readonly заводить учётки не может.
	return slices.Contains(claims.Roles, "admin") && !slices.Contains(claims.Roles, "readonly")
}

func readiness(db *dbclient.Client, vk *valkey.Client) observability.ReadyFunc {
	return func(ctx context.Context) error {
		if err := db.Ping(ctx); err != nil {
			return err
		}
		return vk.Ping(ctx)
	}
}
