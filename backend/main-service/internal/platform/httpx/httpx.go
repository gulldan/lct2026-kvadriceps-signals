package httpx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
	"uuid"

	"github.com/rs/zerolog"

	"github.com/example/main-service/internal/platform/jsonx"
	"github.com/example/main-service/internal/platform/logger"
)

type Server struct {
	srv *http.Server
	log zerolog.Logger
}

func NewServer(addr string, handler http.Handler, log zerolog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      120 * time.Second,
			IdleTimeout:       90 * time.Second,
		},
		log: log,
	}
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info().Str("addr", s.srv.Addr).Msg("http server listening")
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return fmt.Errorf("http serve: %w", err)
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(sctx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	}
}

// Chain: первая обёртка — внешняя.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

type reqIDKey struct{}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewV4().String()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqIDKey{}, id)))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey{}).(string); ok {
		return id
	}
	return ""
}

func Recover(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error().Interface("panic", rec).Str("path", r.URL.Path).Msg("recovered panic")
					Error(w, http.StatusInternalServerError, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func LogRequests(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rl := log.With().Str("request_id", RequestIDFromContext(r.Context())).Logger()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK, wrote: false}
			next.ServeHTTP(sw, r.WithContext(logger.Into(r.Context(), rl)))
			rl.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", sw.status).
				Dur("took", time.Since(start)).
				Msg("request")
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write response: %w", err)
	}
	return n, nil
}

// Flush нужен потоковым обработчикам, чтобы обёртка не прятала http.Flusher.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack нужен апгрейду до WebSocket: gorilla/websocket требует http.Hijacker.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("hijack connection: %w", err)
	}
	w.wrote = true // соединение забрано, писать в него больше нельзя
	return conn, rw, nil
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = jsonx.NewEncoder(w).Encode(v)
	}
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

func Decode(r *http.Request, v any) error {
	dec := jsonx.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}
