package main

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// TestRunStorageInitFails: без ретраев и метаданных EC2 storage.New падает за миллисекунды, а не ждёт.
func TestRunStorageInitFails(t *testing.T) {
	t.Setenv("AWS_MAX_ATTEMPTS", "1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("S3_ENDPOINT", "http://127.0.0.1:1/")
	t.Setenv("S3_ACCESS_KEY", "test")
	t.Setenv("S3_SECRET_KEY", "test")
	t.Setenv("S3_USE_PATH_STYLE", "true")

	err := run(zerolog.Nop())
	if err == nil {
		t.Fatalf("run expected storage init error, got nil")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("run error = %v, want object-store init failure", err)
	}
}
