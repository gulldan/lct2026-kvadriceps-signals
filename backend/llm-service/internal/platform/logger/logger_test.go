package logger_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/example/llm-service/internal/platform/logger"
)

func TestNew(t *testing.T) {
	var buf strings.Builder
	l := logger.New("svc", "info").Output(&buf)
	l.Info().Msg("hello")
	out := buf.String()
	if !strings.Contains(out, `"service":"svc"`) {
		t.Fatalf("log = %q, want it to carry the service field", out)
	}
	if !strings.Contains(out, `"message":"hello"`) {
		t.Fatalf("log = %q, want it to carry the message", out)
	}
}

func TestNewLevelFiltering(t *testing.T) {
	cases := []struct {
		level     string
		emitDebug bool
	}{
		{"debug", true},
		{"info", false},
		{"warn", false},
		{"warning", false},
		{"error", false},
		{"", false},         // пусто — info
		{"  DEBUG  ", true}, // обрезается и приводится к нижнему регистру
		{"nonsense", false}, // неизвестное — info
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			var buf strings.Builder
			l := logger.New("svc", tc.level).Output(&buf)
			l.Debug().Msg("dbg")
			got := strings.Contains(buf.String(), "dbg")
			if got != tc.emitDebug {
				t.Fatalf("level %q debug emitted = %v, want %v (log=%q)", tc.level, got, tc.emitDebug, buf.String())
			}
		})
	}
}

func TestIntoFromRoundTrip(t *testing.T) {
	var buf strings.Builder
	stored := zerolog.New(&buf).With().Str("marker", "carried").Logger()
	ctx := logger.Into(context.Background(), stored)

	got := logger.From(ctx)
	got.Info().Msg("x")
	if !strings.Contains(buf.String(), `"marker":"carried"`) {
		t.Fatalf("From did not return the stored logger; log=%q", buf.String())
	}
}

func TestFromFallback(t *testing.T) {
	l := logger.From(context.Background())
	if l == nil {
		t.Fatal("From returned nil for an empty context")
	}
	// Запасной логгер пишет в stderr; перенаправляем вывод, чтобы проверка не зависела от окружения.
	var buf strings.Builder
	red := l.Output(&buf)
	red.Info().Msg("fallback")
	out := buf.String()
	if !strings.Contains(out, `"service":"unknown"`) {
		t.Fatalf("fallback log = %q, want the unknown service tag", out)
	}
	if !strings.Contains(out, `"message":"fallback"`) {
		t.Fatalf("fallback log = %q, want the emitted message", out)
	}
}

func TestFromFallbackFiltersDebug(t *testing.T) {
	var buf strings.Builder
	red := logger.From(context.Background()).Output(&buf)
	red.Debug().Msg("should-not-appear")
	if strings.Contains(buf.String(), "should-not-appear") {
		t.Fatalf("fallback logger emitted a debug event: %q", buf.String())
	}
}

func TestIntoReturnsDerivedContext(t *testing.T) {
	parent := context.Background()
	child := logger.Into(parent, zerolog.Nop())
	if child == parent {
		t.Fatal("Into returned the parent context unchanged")
	}
}
