package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/provider"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		return healthcheck(ctx)
	}
	var scope provider.Scope
	var output string
	var providerName string
	var limit int
	var timeout time.Duration
	var ingest, once, coverage bool
	var schedule, testRun, manifest string
	flag.BoolVar(&ingest, "ingest", false, "use the durable DB/S3/MQ ingestion pipeline")
	flag.BoolVar(&coverage, "coverage", false, "print durable per-scope coverage manifests for --schedule")
	flag.BoolVar(&once, "once", false, "run one bounded ingestion batch and exit")
	flag.StringVar(&schedule, "schedule", "", "JSON file with explicit provider scopes")
	flag.StringVar(&testRun, "test-run-id", "", "isolated tr-test- namespace for a bounded verification run")
	flag.StringVar(&manifest, "manifest", "", "new local pre-commit manifest for --test-run-id")
	flag.StringVar(&providerName, "provider", "crossref", "crossref, arxiv, github, patent or web")
	flag.StringVar(&scope.Query, "query", "", "provider phrase query; relevance must be checked separately")
	flag.StringVar(&scope.Category, "category", "", "optional arXiv category, such as cs.IR; other providers reject it")
	flag.StringVar(&scope.From, "from", "", "first publication day, YYYY-MM-DD")
	flag.StringVar(&scope.Through, "through", "", "last publication day, YYYY-MM-DD")
	flag.StringVar(&scope.WorkType, "work-type", "journal-article", "journal-article, proceedings-article or posted-content")
	flag.StringVar(&scope.Sort, "sort", "", "GitHub collection order: updated (default) or stars; not a trend score")
	flag.IntVar(&scope.PageSize, "page-size", 50, "records per page, 1..100")
	flag.IntVar(&limit, "limit", 200, "maximum received records per pilot run, 1..200")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "maximum collection duration, at most 15m")
	flag.StringVar(&output, "output", "", "new output directory; existing directories are not overwritten")
	flag.Parse()
	if coverage {
		return printCoverage(ctx, schedule)
	}
	if ingest && !once {
		_ = os.Remove(progressPath)
		defer func() { _ = os.Remove(progressPath) }()
	}
	if ingest {
		scopes := []scheduledScope{{Provider: providerName, Scope: harvest.Scope(scope), Limit: limit}}
		if schedule != "" {
			var err error
			scopes, err = loadSchedule(schedule)
			if err != nil {
				return err
			}
		}
		return runIngestion(ctx, scopes, once, timeout, testRun, manifest)
	}
	if err := scope.ValidateFor(providerName); err != nil {
		return err
	}
	if output == "" || limit < 1 || limit > 200 {
		return errors.New("output directory and limit 1..200 are required")
	}
	if timeout <= 0 || timeout > 15*time.Minute {
		return errors.New("timeout must be positive and at most 15m")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	adapter, adapterErr := newAdapter(providerName)
	if adapterErr != nil {
		return adapterErr
	}
	return collectRecorded(ctx, adapter, scope, limit, output)
}

func collectRecorded(ctx context.Context, adapter provider.Adapter, scope provider.Scope, limit int, output string) error {
	if err := os.Mkdir(output, 0o750); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	root, openErr := os.OpenRoot(output)
	if openErr != nil {
		return fmt.Errorf("open run directory: %w", openErr)
	}
	defer func() { _ = root.Close() }()
	pageNumber := 0
	sink := func(page provider.Page) error {
		pageNumber++
		// Переименование публикует страницу и курсор разом: неполное сырьё не станет точкой перезапуска.
		raw, err := json.Marshal(page)
		if err != nil {
			return fmt.Errorf("encode page: %w", err)
		}
		name := fmt.Sprintf("page-%04d.json", pageNumber)
		if err := root.WriteFile(name+".pending", raw, 0o600); err != nil {
			return fmt.Errorf("write page: %w", err)
		}
		if err := root.Rename(name+".pending", name); err != nil {
			return fmt.Errorf("publish page: %w", err)
		}
		return nil
	}
	result, collectErr := provider.Collect(ctx, adapter, scope, limit, sink)
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := root.WriteFile("manifest.json", raw, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if _, err := fmt.Fprintln(os.Stdout, string(raw)); err != nil {
		return fmt.Errorf("write collection report: %w", err)
	}
	return collectErr
}
