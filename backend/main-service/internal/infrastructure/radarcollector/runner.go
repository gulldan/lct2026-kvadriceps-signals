package radarcollector

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/example/main-service/internal/application"
)

const fullInterval = 6 * time.Hour
const scienceJob = "science"

// Run: научный сбор и починка вспомогательных снимков берут одну распределённую аренду.
func Run(ctx context.Context, live *application.TrendLiveService, coordinator *Coordinator, once, sourcesOnly bool) error {
	live.SetWriterGuard(coordinator.Acquire)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		wait, err := tick(ctx, live, coordinator, once, sourcesOnly)
		if once || sourcesOnly {
			return err
		}
		if err != nil && !errors.Is(err, application.ErrLiveBusy) {
			slog.Error("radar cycle", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

func tick(parent context.Context, live *application.TrendLiveService, coordinator *Coordinator,
	once, sourcesOnly bool) (time.Duration, error) {
	ctx, release, err := coordinator.Acquire(parent)
	if err != nil {
		return time.Minute, err
	}
	defer release()
	state, err := coordinator.Status(ctx)
	if err != nil {
		return time.Minute, err
	}
	if state == nil {
		state = &application.LiveCollectionStatus{}
	}
	now := time.Now().UTC()
	delay := live.RefreshDelay(ctx, fullInterval)
	overview := live.Overview()
	next := resumeStatus(state, overview, now, delay)
	state.Channels = channels(ctx, live)
	kind := ""
	if once || (!now.Before(next) && !sourcesOnly) {
		kind = scienceJob
	} else if sourcesOnly || needsRecovery(state.Channels) {
		kind = "sources"
	}
	var jobErr error
	if kind != "" {
		next, jobErr = collect(ctx, live, coordinator, state, kind, next)
	}

	state.Phase, state.NextRunAt = idlePhase, next.UTC().Format(time.RFC3339)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	// Квитанция сохраняется до освобождения той же аренды, под которой шла публикация.
	return min(time.Minute, max(time.Second, time.Until(next))), errors.Join(jobErr, coordinator.Save(saveCtx, state))
}

func resumeStatus(state *application.LiveCollectionStatus, overview application.LiveOverview,
	now time.Time, delay time.Duration) time.Time {
	next, err := time.Parse(time.RFC3339, state.NextRunAt)
	if err != nil {
		next = now.Add(delay)
	}
	if state.Phase != idlePhase && len(state.Runs) > 0 && state.Runs[len(state.Runs)-1].FinishedAt == "" {
		interrupted := &state.Runs[len(state.Runs)-1]
		interrupted.FinishedAt, interrupted.Error = now.Format(time.RFC3339), "writer interrupted; publication not confirmed"
		if interrupted.Kind == scienceJob {
			next = now
		}
	}
	state.CheckedAt = now.Format(time.RFC3339)
	if state.LastSuccessAt == "" {
		observed, parseErr := time.Parse(time.RFC3339, overview.ObservedAt)
		duration, durationErr := time.ParseDuration(overview.Duration)
		if parseErr == nil && durationErr == nil {
			state.LastSuccessAt = observed.Add(duration).Format(time.RFC3339)
		}
	}
	return next
}

func collect(ctx context.Context, live *application.TrendLiveService, coordinator *Coordinator,
	state *application.LiveCollectionStatus, kind string, next time.Time) (time.Time, error) {
	state.Phase = kind
	run := application.LiveCollectorRun{Kind: kind, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	state.Runs = append(state.Runs, run)
	state.Runs = state.Runs[max(0, len(state.Runs)-32):]
	if err := coordinator.Save(ctx, state); err != nil {
		return next, err
	}
	timeout := jobLimit
	if kind != scienceJob {
		// Добор источников опрашивает три сборщика по сотне направлений и пересчитывает
		// свидетельства, поэтому в пять минут он не укладывается.
		timeout = 20 * time.Minute
	}
	jobCtx, stop := context.WithTimeout(ctx, timeout)
	var jobErr error
	if kind == scienceJob {
		jobErr = live.Refresh(jobCtx)
	} else {
		jobErr = live.RefreshSources(jobCtx)
	}
	stop()
	run.FinishedAt, run.Success = time.Now().UTC().Format(time.RFC3339), jobErr == nil
	if jobErr != nil {
		run.Error = jobErr.Error()
	}
	// Ошибка научного прохода гасится только научным успехом: удачный добор
	// патентов и веба не делает вчерашний снимок свежим.
	if kind == scienceJob {
		state.Error = run.Error
	}
	if jobErr == nil && kind == scienceJob {
		state.LastSuccessAt = run.FinishedAt
	}
	if jobErr == nil && kind != scienceJob {
		state.LastSourceAt = run.FinishedAt
	}
	state.Runs[len(state.Runs)-1] = run
	if kind == scienceJob {
		delay := fullInterval
		if jobErr != nil {
			delay = 15 * time.Minute
		}
		next = time.Now().Add(delay)
	}
	state.Channels = channels(ctx, live)
	return next, jobErr
}

func needsRecovery(channels []application.LiveCollectorChannel) bool {
	for _, channel := range channels {
		observed, err := time.Parse(time.RFC3339, channel.ObservedAt)
		attached, attachedErr := time.Parse(time.RFC3339, channel.AttachedAt)
		changed := attachedErr != nil || observed.After(attached) || channel.SourceSet != channel.AttachedSourceSet
		if channel.Status == "ready" && channel.Error == "" && err == nil && changed {
			return true
		}
	}
	return false
}

func channels(ctx context.Context, live *application.TrendLiveService) []application.LiveCollectorChannel {
	result := []application.LiveCollectorChannel{}
	for _, source := range []struct{ name, env string }{
		{"patents", "PATENT_COLLECTOR_URL"}, {"web", "WEB_COLLECTOR_URL"}, {"patents_cn", "CN_PATENT_COLLECTOR_URL"},
	} {
		base := strings.TrimRight(os.Getenv(source.env), "/")
		if base == "" {
			continue
		}
		channel := live.CollectedChannel(source.name)
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		status, err := probeChannel(probeCtx, base)
		cancel()
		if err != nil {
			channel.Status, channel.Error = "unavailable", err.Error()
		} else {
			channel.Status, channel.ObservedAt, channel.NextRunAt = status.Status, status.ObservedAt, status.NextRunAt
			channel.Error, channel.Collecting, channel.SourceSet = status.Error, status.Collecting, status.SourceSet
		}
		result = append(result, channel)
	}
	return result
}

func probeChannel(ctx context.Context, base string) (application.LiveCollectorChannel, error) {
	status := application.LiveCollectorChannel{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/status", http.NoBody)
	if err != nil {
		return status, fmt.Errorf("source status request: %w", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return status, fmt.Errorf("source status: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return status, fmt.Errorf("source status: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return status, fmt.Errorf("read source status: %w", err)
	}
	if err = json.Unmarshal(raw, &status); err != nil {
		return status, fmt.Errorf("decode source status: %w", err)
	}
	return status, nil
}
