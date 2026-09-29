package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/main-service/internal/platform/httpx"
)

// workerActivityView: всё, кроме Name, — указатели, чтобы не отчитавшийся воркер давал null, а не ложные нули.
type workerActivityView struct {
	Name           string   `json:"name"`
	State          *string  `json:"state"`
	Epoch          *int64   `json:"epoch"`
	ProgressDone   *int64   `json:"progress_done"`
	ProgressTotal  *int64   `json:"progress_total"`
	UpdatedAt      *string  `json:"updated_at"`
	LastError      *string  `json:"last_error"`
	LastRunSeconds *float64 `json:"last_run_seconds"`
	// LastSuccessAt — последний чистый прогон (свежесть доски), в отличие от UpdatedAt — последнего пульса.
	LastSuccessAt *string `json:"last_success_at"`
}

type systemActivityView struct {
	CorpusEpoch *int64               `json:"corpus_epoch"`
	Workers     []workerActivityView `json:"workers"`
	LastEpochs  map[string]*int64    `json:"last_epochs"`
}

// systemActivity не падает: нет ключа или сбой Valkey дают null, а не ошибку.
func (a *API) systemActivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	// Порядок — порядок показа. Пульс лежит в rag:worker:<name>:status; у itc и eval запасное состояние —
	// текстовый rag:<name>:last_status.
	workers := []string{keyClusters, "discovery", "raptor", keyITC, "eval"}
	ctx := r.Context()
	view := systemActivityView{
		CorpusEpoch: a.activityInt(ctx, "rag:corpus_epoch:shared"),
		Workers:     make([]workerActivityView, 0, len(workers)),
		LastEpochs: map[string]*int64{
			keyClusters: a.activityInt(ctx, "rag:clusters:last_epoch"),
			"discovery": a.activityInt(ctx, "rag:discovery:last_epoch"),
			"raptor":    a.activityInt(ctx, "rag:raptor:last_epoch"),
		},
	}
	for _, name := range workers {
		ws := workerActivityView{Name: name}
		if raw, found, err := a.metrics.Get(ctx, "rag:worker:"+name+":status"); err == nil && found {
			_ = json.Unmarshal([]byte(raw), &ws)
			ws.Name = name
		}
		if ws.State == nil && (name == keyITC || name == "eval") {
			if raw, found, err := a.metrics.Get(ctx, "rag:"+name+":last_status"); err == nil && found {
				s := strings.TrimSpace(raw)
				ws.State = &s
			}
		}
		view.Workers = append(view.Workers, ws)
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (a *API) activityInt(ctx context.Context, key string) *int64 {
	raw, found, err := a.metrics.Get(ctx, key)
	if err != nil || !found {
		return nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
