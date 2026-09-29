package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

type Metrics struct {
	reg      *prometheus.Registry
	messages *prometheus.CounterVec
	duration *prometheus.HistogramVec
	stages   *prometheus.HistogramVec
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rag_messages_processed_total",
			Help: "Total AMQP messages processed, by queue and outcome.",
		}, []string{"queue", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rag_processing_duration_seconds",
			Help:    "Message handling latency in seconds, by queue.",
			Buckets: prometheus.DefBuckets,
		}, []string{"queue"}),
		// Корзины до 120 с: хостинговая генерация бывает медленной.
		stages: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rag_stage_duration_seconds",
			Help:    "RAG query stage latency in seconds, by pipeline stage.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		}, []string{"stage"}),
	}
	reg.MustRegister(m.messages, m.duration, m.stages, collectors.NewGoCollector())
	return m
}

func (m *Metrics) RecordMessage(queue, outcome string, seconds float64) {
	m.messages.WithLabelValues(queue, outcome).Inc()
	m.duration.WithLabelValues(queue).Observe(seconds)
}

func (m *Metrics) RecordStage(stage string, seconds float64) {
	m.stages.WithLabelValues(stage).Observe(seconds)
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

type ReadyFunc func(ctx context.Context) error

func RunOps(ctx context.Context, addr string, m *Metrics, ready ReadyFunc, log zerolog.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeText(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				writeText(w, http.StatusServiceUnavailable, err.Error())
				return
			}
		}
		writeText(w, http.StatusOK, "ready")
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", addr).Msg("ops server listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("ops server: %w", err)
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			return fmt.Errorf("ops shutdown: %w", err)
		}
		return nil
	}
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}
