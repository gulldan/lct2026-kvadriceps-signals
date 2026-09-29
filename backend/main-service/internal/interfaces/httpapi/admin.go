package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) adminUsers(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin") {
		return
	}
	users, err := a.admin.ListUsers(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newUserViews(users))
}

func (a *API) postMetrics(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "automation") {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "read body")
		return
	}
	if !json.Valid(body) {
		httpx.Error(w, http.StatusBadRequest, "scorecard must be valid JSON")
		return
	}
	wrapped := fmt.Sprintf(`{"stored_at":%q,"scorecard":%s}`, time.Now().UTC().Format(time.RFC3339), body)
	if serr := a.metrics.Set(r.Context(), evalScorecardKey, wrapped, 0); serr != nil {
		a.fail(w, serr)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "stored"})
}

func (a *API) getMetrics(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	raw, found, err := a.metrics.Get(r.Context(), evalScorecardKey)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !found {
		_, _ = w.Write([]byte(`{"scorecard":null}`))
		return
	}
	_, _ = w.Write([]byte(raw))
}

func (a *API) triggerMetricsRun(w http.ResponseWriter, r *http.Request) {
	a.triggerWorker(w, r, evalTriggerKey)
}

func (a *API) triggerITCRun(w http.ResponseWriter, r *http.Request) {
	a.triggerWorker(w, r, itcTriggerKey)
}

func (a *API) triggerWorker(w http.ResponseWriter, r *http.Request, key string) {
	if !a.requireRole(w, r, "admin") {
		return
	}
	triggeredAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := a.metrics.EnqueueWork(r.Context(), key, triggeredAt); err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{
		"status":       "queued",
		"triggered_at": triggeredAt,
	})
}
