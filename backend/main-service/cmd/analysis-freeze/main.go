// Command analysis-freeze замораживает один явно заданный авторизованный вход корпуса; темы не ранжирует
// и завершённым анализом трендов себя не выдаёт.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/storage"
)

func main() {
	requestPath := flag.String("request", "", "explicit reader/query/cutoff request JSON")
	outputPath := flag.String("output", "", "new local canonical manifest file")
	flag.Parse()
	if err := run(*requestPath, *outputPath); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(requestPath, outputPath string) error {
	raw, err := os.ReadFile(requestPath) // #nosec G304 -- operator-selected bounded request file.
	if err != nil {
		return fmt.Errorf("read analysis request: %w", err)
	}
	request, err := analysisdoc.Decode[analysisdoc.Request](string(raw))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	objects, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}
	frozen, err := application.FreezeAnalysis(ctx, db, objects, request)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- operator chooses a new file.
	if err != nil {
		return fmt.Errorf("create analysis manifest: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err = file.WriteString(frozen.JSON); err != nil {
		return fmt.Errorf("write analysis manifest: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "prepared input: run=%s sha256=%s bytes=%d\n", frozen.RunID, frozen.SHA256, len(frozen.JSON))
	return nil
}
