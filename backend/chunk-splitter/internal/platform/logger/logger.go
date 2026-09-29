package logger

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

type ctxKey struct{}

func New(service, level string) zerolog.Logger {
	return build(os.Stdout, service, level)
}

func build(w io.Writer, service, level string) zerolog.Logger {
	return zerolog.New(w).
		Level(parseLevel(level)).
		With().
		Timestamp().
		Str("service", service).
		Logger()
}

func parseLevel(s string) zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zerolog.DebugLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

func Into(ctx context.Context, l zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

func From(ctx context.Context) *zerolog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(zerolog.Logger); ok {
		return &l
	}
	fb := build(os.Stderr, "unknown", "info")
	return &fb
}
