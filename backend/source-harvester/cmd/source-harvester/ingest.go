package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"uuid"

	"github.com/example/source-harvester/internal/platform/config"
	harvestv1 "github.com/example/source-harvester/internal/platform/genproto/harvest/v1"
	"github.com/example/source-harvester/internal/platform/grpcx"
	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/logger"
	"github.com/example/source-harvester/internal/platform/messaging"
	"github.com/example/source-harvester/internal/platform/storage"
	"github.com/example/source-harvester/internal/provider"
	"github.com/example/source-harvester/internal/worker"
)

type scheduledScope struct {
	Industry      string        `json:"industry,omitempty"`
	Provider      string        `json:"provider"`
	CurrentMonth  bool          `json:"current_month,omitempty"`
	HistoryMonths int           `json:"history_months,omitzero"`
	Scope         harvest.Scope `json:"scope"`
	Limit         int           `json:"limit"`
}

func newAdapter(name string) (provider.Adapter, error) {
	switch name {
	case "crossref":
		return provider.NewCrossref(os.Getenv("PUBSEARCH_MAILTO")), nil
	case "arxiv":
		return provider.NewArXiv(), nil
	case "github":
		return provider.NewGitHub(os.Getenv("GITHUB_TOKEN")), nil
	case "patent", "web":
		return provider.NewCollector(name)
	default:
		return nil, errors.New("provider must be crossref, arxiv, github, patent or web")
	}
}

func runIngestion(
	ctx context.Context, scopes []scheduledScope, once bool,
	timeout time.Duration, testRun, manifestPath string,
) error {
	if err := validateIngestion(scopes, once, timeout, testRun, manifestPath); err != nil {
		return ingestionError(err)
	}
	owner := os.Getenv("HARVEST_OWNER_ID")
	interval, err := time.ParseDuration(config.Get("HARVEST_INTERVAL", "6h"))
	if err != nil || interval < time.Hour {
		return errors.New("HARVEST_INTERVAL must be at least one hour")
	}
	log := logger.New("source-harvester", config.Get("LOG_LEVEL", "info"))
	conn, err := grpcx.Dial(config.Get("DB_SERVICE_ADDR", "db-service:9091"))
	if err != nil {
		return ingestionError(err)
	}
	defer func() { _ = conn.Close() }()
	objects, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return ingestionError(err)
	}
	mq, err := messaging.Dial(ctx, config.MustGet("RABBITMQ_URL"), log)
	if err != nil {
		return ingestionError(err)
	}
	defer func() { _ = mq.Close() }()
	if err = mq.DeclareTopology(); err != nil {
		return ingestionError(err)
	}
	publisher, err := mq.NewPublisher()
	if err != nil {
		return ingestionError(err)
	}
	defer func() { _ = publisher.Close() }()
	adapters, err := configuredAdapters(scopes)
	if err != nil {
		return err
	}
	control := worker.Client{RPC: harvestv1.NewHarvestServiceClient(conn)}
	scopes = append(scopes, requestedScopes(ctx, control, scopes, once)...)
	return repeatScopes(ctx, scopes, once, interval, trackProgress(once, timeout, interval, func(s scheduledScope) error {
		coverageKey := scopeCoverageKey(owner, testRun, s)
		s, previous, coverageErr := prepareScopeCoverage(ctx, objects, coverageKey, s)
		if coverageErr != nil {
			return coverageErr
		}
		in := harvest.Start{
			OwnerID:   owner,
			Provider:  s.Provider,
			Scope:     s.Scope,
			Limit:     s.Limit,
			RunID:     uuid.NewV4().String(),
			Token:     uuid.NewV4().String(),
			TestRunID: testRun,
		}
		w := worker.Worker{
			Control:   worker.Client{RPC: harvestv1.NewHarvestServiceClient(conn)},
			Objects:   objects,
			Publisher: worker.Ingestion{Publisher: publisher},
			Adapter:   adapters[s.Provider],
		}
		if testRun != "" {
			sink, manifestErr := testManifest(manifestPath, in)
			if manifestErr != nil {
				return ingestionError(manifestErr)
			}
			w.BeforeCommit = sink
		}
		jobCtx, cancel := context.WithTimeout(ctx, timeout)
		job, err := w.Run(jobCtx, in)
		cancel()
		coverage := coverageOutcome(previous, s, job, err)
		if manifestErr := finishScopeCoverage(ctx, objects, coverageKey, coverage, w.Adapter); manifestErr != nil {
			return ingestionError(errors.Join(err, manifestErr))
		}
		if err != nil {
			log.Error().Err(err).Str("provider", s.Provider).Str("scope_id", in.ScopeID()).Msg(
				"collection failed; checkpoint retained",
			)
			return ingestionError(err)
		}
		log.Info().Str("provider", s.Provider).Str("status", job.Status).Bool("incomplete", job.Incomplete).Int(
			"received",
			job.Received,
		).Str(
			"scope_id",
			job.Lease.ScopeID,
		).Msg(
			"collection batch finished",
		)
		return nil
	}))
}

func loadSchedule(path string) ([]scheduledScope, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- explicit operator CLI path, never provider or HTTP input.
	if err != nil {
		return nil, ingestionError(err)
	}
	if len(raw) > 64<<10 {
		return nil, errors.New("schedule exceeds 64 KiB")
	}
	var scopes []scheduledScope
	err = json.Unmarshal(raw, &scopes, json.RejectUnknownMembers(true))
	return scopes, ingestionError(err)
}

func testManifest(name string, in harvest.Start) (func(harvest.CommitPage) error, error) {
	type manifest struct {
		Version int      `json:"version"`
		TestRun string   `json:"test_run_id"`
		Owner   string   `json:"owner_id"`
		ScopeID string   `json:"scope_id"`
		RunID   string   `json:"run_id"`
		IDs     []string `json:"ids"`
		Objects []string `json:"object_keys"`
	}
	m := manifest{
		Version: 1,
		TestRun: in.TestRunID,
		Owner:   in.OwnerID,
		ScopeID: in.ScopeID(),
		RunID:   in.RunID,
		IDs:     []string{},
		Objects: []string{},
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- explicit operator manifest destination.
	if err != nil {
		return nil, fmt.Errorf("create test manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, ingestionError(err)
	}
	write := func() error {
		raw, err := json.Marshal(m)
		if err != nil {
			return ingestionError(err)
		}
		temp, err := os.CreateTemp(filepath.Dir(name), ".harvest-manifest-")
		if err != nil {
			return ingestionError(err)
		}
		defer func() { _ = temp.Close(); _ = os.Remove(temp.Name()) }()
		if _, err := temp.Write(raw); err != nil {
			return ingestionError(err)
		}
		if err := temp.Sync(); err != nil {
			return ingestionError(err)
		}
		if err := temp.Close(); err != nil {
			return ingestionError(err)
		}
		return os.Rename(temp.Name(), name)
	}
	if err := write(); err != nil {
		return nil, ingestionError(err)
	}
	return func(page harvest.CommitPage) error {
		for _, r := range page.Records {
			m.IDs = append(m.IDs, r.DocumentID)
			m.Objects = append(m.Objects, r.ObjectKey)
		}
		m.Objects = append(m.Objects, page.RawKey, page.ManifestKey)
		if len(m.IDs) > 50 {
			return errors.New("test manifest exceeded 50 documents")
		}
		return write()
	}, nil
}

func validateIngestion(scopes []scheduledScope, once bool, timeout time.Duration, testRun, manifestPath string) error {
	owner := os.Getenv("HARVEST_OWNER_ID")
	if len(scopes) == 0 || len(scopes) > 64 || timeout <= 0 || timeout > 15*time.Minute {
		return errors.New("ingestion requires 1..64 bounded scopes and a timeout up to 15m")
	}
	if testRun != "" && (!once || len(scopes) != 1 || manifestPath == "") {
		return errors.New("test ingestion requires --once, one scope and a new --manifest file")
	}
	for _, s := range scopes {
		s = scopeAt(s, time.Now())
		probe := harvest.Start{
			OwnerID:   owner,
			Provider:  s.Provider,
			Scope:     s.Scope,
			Limit:     s.Limit,
			RunID:     uuid.NewV4().String(),
			Token:     uuid.NewV4().String(),
			TestRunID: testRun,
		}
		if err := probe.Validate(); err != nil {
			return ingestionError(err)
		}
	}

	return nil
}

// requestedScopes пропускает недоступный контур управления: запрос пользователя не должен остановить плановый сбор.
func requestedScopes(ctx context.Context, control worker.Control, configured []scheduledScope, once bool) []scheduledScope {
	if once {
		return nil
	}
	providers := map[string]bool{}
	for _, scope := range configured {
		providers[scope.Provider] = true
	}
	for _, provider := range []string{"crossref", "arxiv", "github", "patent", "web"} {
		providers[provider] = true
	}
	out := []scheduledScope{}
	for provider := range providers {
		collections, err := control.Requested(ctx, harvest.Requested{Provider: provider, Limit: 25})
		if err != nil {
			continue
		}
		for _, collection := range collections {
			out = append(out, scheduledScope{Provider: collection.Provider,
				Scope: collection.Scope, Limit: collection.Limit})
		}
	}
	return out
}

func repeatScopes(
	ctx context.Context, scopes []scheduledScope, once bool,
	interval time.Duration, run func(scheduledScope) error,
) error {
	for {
		var roundErr error
		for _, scope := range scopes {
			if err := run(scopeAt(scope, time.Now())); err != nil {
				roundErr = err
			}
			if ctx.Err() != nil {
				return fmt.Errorf("scheduled collection: %w", ctx.Err())
			}
		}
		if once {
			return roundErr
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("scheduled collection: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func ingestionError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("ingestion: %w", err)
}

func configuredAdapters(scopes []scheduledScope) (map[string]provider.Adapter, error) {
	adapters := map[string]provider.Adapter{}
	for _, s := range scopes {
		if s.HistoryMonths < 0 || s.HistoryMonths > 36 || (s.CurrentMonth && s.HistoryMonths > 0) {
			return nil, errors.New("history_months must be 0..36 and cannot accompany current_month")
		}
		if adapters[s.Provider] != nil {
			continue
		}
		a, err := newAdapter(s.Provider)
		if err != nil {
			return nil, ingestionError(err)
		}
		adapters[s.Provider] = a
	}

	return adapters, nil
}

// scopeAt пересчитывает даты каждый раунд, чтобы долгоживущий воркер не застрял на прошлом месяце.
func scopeAt(s scheduledScope, now time.Time) scheduledScope {
	if s.CurrentMonth || s.HistoryMonths > 0 {
		now = now.UTC()
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		s.Scope.From = month.AddDate(0, -s.HistoryMonths, 0).Format(time.DateOnly)
		s.Scope.Through = now.Format(time.DateOnly)
	}
	return s
}
