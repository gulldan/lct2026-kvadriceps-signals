package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/storage"
	"github.com/example/source-harvester/internal/provider"
)

type scopeCoverage struct {
	Version    int           `json:"version"`
	Provider   string        `json:"provider"`
	Industry   string        `json:"industry"`
	Scope      harvest.Scope `json:"scope"`
	ScopeID    string        `json:"scope_id"`
	SourceSet  string        `json:"source_set"`
	RunID      string        `json:"run_id"`
	Received   int           `json:"received_this_batch"`
	Pages      int           `json:"pages_this_batch"`
	Cursor     string        `json:"next_cursor"`
	Exhausted  bool          `json:"provider_search_exhausted"`
	Incomplete bool          `json:"incomplete_source_coverage"`
	StopReason string        `json:"stop_reason"`
	ObservedAt time.Time     `json:"observed_at"`
}

func scopeCoverageKey(owner, testRun string, s scheduledScope) string {
	if s.CurrentMonth || s.HistoryMonths > 0 {
		s.Scope.From = ""
		s.Scope.Through = ""
	}
	s.CurrentMonth = false
	s.Limit = 0
	s.Industry = ""
	// Даты текущего месяца исключены только из ID манифеста планировщика; ID областей в базе хранят точный интервал.
	raw, _ := json.Marshal(struct {
		Owner, TestRun string
		Scope          scheduledScope
	}{owner, testRun, s})
	sum := sha256.Sum256(raw)
	return "sources/coverage/" + hex.EncodeToString(sum[:]) + ".json"
}
func readScopeCoverage(ctx context.Context, objects *storage.Client, key string) (scopeCoverage, error) {
	var value scopeCoverage
	f, err := objects.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return value, nil
	}
	if err != nil {
		return value, ingestionError(err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return value, ingestionError(err)
	}
	if len(raw) > 64<<10 {
		return value, errors.New("scope manifest exceeds64KiB")
	}
	err = json.Unmarshal(raw, &value, json.RejectUnknownMembers(true))
	return value, ingestionError(err)
}
func saveScopeCoverage(ctx context.Context, objects *storage.Client, key string, value scopeCoverage) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return ingestionError(err)
	}
	return objects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw)), "application/json")
}
func resumeScheduledWindow(scope scheduledScope, previous scopeCoverage) scheduledScope {
	// Недобранный по бюджету вчерашний запрос дочитывается до сдвига Through, прошлый месяц — при смене календаря.
	if (scope.CurrentMonth || scope.HistoryMonths > 0) && previous.Version == 1 && !previous.Exhausted &&
		previous.Scope.From != "" &&
		previous.Scope.Through <= scope.Scope.Through {
		scope.Scope = previous.Scope
	}
	return scope
}
func coverageOutcome(previous scopeCoverage, s scheduledScope, job harvest.Job, runErr error) scopeCoverage {
	c := scopeCoverage{Version: 1, Provider: s.Provider, Industry: s.Industry, Scope: s.Scope,
		SourceSet: previous.SourceSet, ObservedAt: time.Now().UTC(), StopReason: "running"}
	if job.Lease.ScopeID == "" && runErr != nil {
		previous.StopReason = "failed_before_lease; checkpoint retained"
		previous.ObservedAt = c.ObservedAt
		return previous
	}
	c.ScopeID = job.Lease.ScopeID
	c.RunID = job.Lease.RunID
	c.Received = job.Received
	c.Pages = job.PageNumber
	c.Cursor = job.Cursor
	c.Exhausted = job.Exhausted
	c.Incomplete = job.Incomplete
	switch {
	case errors.Is(runErr, provider.ErrCursorExpired) && job.Status == "failed" && job.Cursor == "":
		c.StopReason = "source_snapshot_changed; cursor reset, restart next batch"
	case runErr != nil:
		c.StopReason = "source_or_ingestion_failed; checkpoint retained"
	case job.Exhausted:
		c.StopReason = "provider_search_exhausted"
	default:
		c.StopReason = "record_budget_reached; resume next batch"
	}
	if errors.Is(runErr, context.DeadlineExceeded) {
		c.StopReason = "timeout; checkpoint retained"
	}
	return c
}
func printCoverage(ctx context.Context, schedule string) error {
	scopes, err := loadSchedule(schedule)
	if err != nil {
		return ingestionError(err)
	}
	objects, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return ingestionError(err)
	}
	result := []scopeCoverage{}
	for _, s := range scopes {
		c, readErr := readScopeCoverage(ctx, objects, scopeCoverageKey(os.Getenv("HARVEST_OWNER_ID"), "", s))
		if readErr != nil {
			return ingestionError(readErr)
		}
		if c.Version == 0 {
			c = scopeCoverage{Version: 1, Provider: s.Provider, Industry: s.Industry,
				Scope: scopeAt(s, time.Now()).Scope, StopReason: "not_started"}
		}
		result = append(result, c)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return ingestionError(err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(raw))
	return ingestionError(err)
}

func prepareScopeCoverage(ctx context.Context, objects *storage.Client, key string,
	s scheduledScope) (scheduledScope, scopeCoverage, error) {
	previous, err := readScopeCoverage(ctx, objects, key)
	if err != nil {
		return s, previous, err
	}
	s = resumeScheduledWindow(s, previous)
	// Окно фиксируется до аренды в базе, включая первый обход после завершённого дня, чтобы падение не потеряло курсор.
	if previous.Exhausted || previous.Version == 0 {
		previous = scopeCoverage{Version: 1, Provider: s.Provider, Industry: s.Industry, Scope: s.Scope,
			StopReason: "running", ObservedAt: time.Now().UTC(), SourceSet: previous.SourceSet}
	}
	return s, previous, saveScopeCoverage(ctx, objects, key, previous)
}
func finishScopeCoverage(ctx context.Context, objects *storage.Client, key string,
	coverage scopeCoverage, adapter provider.Adapter) error {
	coverageCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if population, ok := adapter.(interface{ PopulationVersion() string }); ok {
		coverage.SourceSet = population.PopulationVersion()
	} else {
		coverage.SourceSet = coverage.Provider + ":public-api-v1"
	}
	if err := saveScopeCoverage(coverageCtx, objects, key, coverage); err != nil {
		return err
	}
	if coverage.RunID != "" {
		return saveScopeCoverage(coverageCtx, objects, "sources/coverage/runs/"+coverage.RunID+".json", coverage)
	}
	return nil
}
