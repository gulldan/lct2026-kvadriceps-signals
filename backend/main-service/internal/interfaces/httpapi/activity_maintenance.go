package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	maintenanceQueued    = "queued"
	maintenanceSucceeded = "succeeded"
)

// Исполнитель индекса пишет журнал задания, а старый снимок пульса больше не обновляет.
// Даты берутся из журнала; наличие аренды подтверждает, что текущий процесс ещё жив.
func (a *API) applyMaintenanceActivity(ctx context.Context, view *workerActivityView, raw string) {
	if view.State == nil {
		view.State = new(strings.TrimSpace(raw))
	}
	var request string
	for field := range strings.FieldsSeq(raw) {
		if value, found := strings.CutPrefix(field, "request="); found {
			request = value
		}
	}
	if request == "" {
		return
	}
	prefix := "rag:work:{" + view.Name + "}"
	key := fmt.Sprintf("%s:%x", prefix, sha256.Sum256([]byte(request)))
	record, err := a.metrics.HGetAll(ctx, key)
	if err != nil || record["request"] != request {
		return
	}
	started := maintenanceTime(record["started"])
	finished := maintenanceTime(record["finished"])
	observed, state := finished, record["state"]
	if state == "running" {
		observed = started
		if _, alive, readErr := a.metrics.Get(ctx, prefix+":lease"); readErr == nil && alive {
			observed = time.Now().UTC()
		} else {
			state = "unknown"
		}
	}
	if observed.IsZero() {
		return
	}
	if view.UpdatedAt != nil {
		previous, _ := time.Parse(time.RFC3339, *view.UpdatedAt)
		if previous.After(observed) {
			return
		}
	}
	view.UpdatedAt = new(observed.Format(time.RFC3339))
	view.ProgressDone, view.ProgressTotal, view.LastError = nil, nil, nil
	view.LastSuccessAt, view.LastRunSeconds = nil, nil
	if state != maintenanceSucceeded {
		a.applyMaintenanceSuccess(ctx, view, prefix)
	}
	switch state {
	case maintenanceSucceeded:
		state = "idle"
		view.LastSuccessAt = new(finished.Format(time.RFC3339))
	case "failed":
		state = "error"
		view.LastError = new(record["error"])
	case maintenanceQueued:
		state = "idle"
	}
	view.State = &state
	if !started.IsZero() && !finished.Before(started) {
		view.LastRunSeconds = new(finished.Sub(started).Seconds())
	}
}

// Последняя попытка заменяет текущий статус, поэтому прошлый успех хранится отдельно.
func (a *API) applyMaintenanceSuccess(ctx context.Context, view *workerActivityView, prefix string) {
	key, found, err := a.metrics.Get(ctx, prefix+":last_success")
	if err != nil || !found || !strings.HasPrefix(key, prefix+":") {
		return
	}
	record, err := a.metrics.HGetAll(ctx, key)
	if err != nil || record["state"] != maintenanceSucceeded || record["request"] == "" ||
		key != fmt.Sprintf("%s:%x", prefix, sha256.Sum256([]byte(record["request"]))) {
		return
	}
	started, finished := maintenanceTime(record["started"]), maintenanceTime(record["finished"])
	if started.IsZero() || finished.Before(started) {
		return
	}
	view.LastSuccessAt = new(finished.Format(time.RFC3339))
	view.LastRunSeconds = new(finished.Sub(started).Seconds())
}

func maintenanceTime(raw string) time.Time {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > float64(time.Now().Unix()+60) {
		return time.Time{}
	}
	seconds, fraction := math.Modf(value)
	return time.Unix(int64(seconds), int64(fraction*float64(time.Second))).UTC()
}
