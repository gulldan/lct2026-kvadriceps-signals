package httpapi

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) SetTrendLive(live *application.TrendLiveService) {
	a.live, a.covers = live, application.NewLiveCovers(live)
}

func (a *API) trendLive(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	// Снимок в сотни килобайт собирается раз в сутки: перезагрузка между сборами должна стоить 304.
	cachedAnalysisJSON(w, r, a.live.Overview())
}

// trendLiveModel кэшируется: веса и отчёт встроены в сборку и меняются только с выкладкой.
func (a *API) trendLiveModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	weights, evaluation, err := application.WeakSignalArtifacts()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	analysisJSON(w, http.StatusOK, struct {
		Model      jsontext.Value `json:"model"`
		Evaluation jsontext.Value `json:"evaluation"`
	}{weights, evaluation})
}

// trendLiveModelCheck отдаёт ошибку разбора файла текстом — интерфейс показывает её как есть.
func (a *API) trendLiveModelCheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, application.WeakSignalFileBytes))
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "file is larger than 2 MiB")
		return
	}
	rows, err := application.ParseWeakSignalFile(data)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	check, err := a.live.StartWeakSignalFileCheck(r.Context(), rows)
	switch {
	case errors.Is(err, application.ErrWeakSignalFileBusy):
		w.Header().Set("Retry-After", "30")
		httpx.Error(w, http.StatusTooManyRequests, "model file check is busy")
		return
	case errors.Is(err, application.ErrLiveNotReady):
		httpx.Error(w, http.StatusServiceUnavailable, "live snapshot is still being collected")
		return
	case err != nil:
		slog.Error("model file check", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "model file check failed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusAccepted, check)
}

func (a *API) trendLiveModelCheckResult(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	check, ok := a.live.WeakSignalFileResult(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "model file check not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, check)
}

func (a *API) trendLiveTopic(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	detail, ok := a.live.OpenTopic(r.Context(), r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "topic not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, detail)
}

func (a *API) trendLiveSearch(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	// Сбор не укладывается в жизнь HTTP-запроса, поэтому поиск отвечает сразу и продолжается в фоне.
	// Владелец нужен каналу загруженных документов; refresh=1 минует кэш и сохранённый ответ.
	state, err := a.live.StartSearch(r.Context(), r.URL.Query().Get("q"), owner, r.URL.Query().Get("refresh") == "1")
	switch {
	case errors.Is(err, application.ErrLiveSearchBusy):
		w.Header().Set("Retry-After", "5")
		httpx.Error(w, http.StatusTooManyRequests, "live search is busy")
		return
	case errors.Is(err, application.ErrLiveNotReady):
		httpx.Error(w, http.StatusServiceUnavailable, "live snapshot is still being collected")
		return
	case err != nil:
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, state)
}

// trendLiveSearchTop не запускает сбор: страница тезиса показывает найденное по его запросам.
func (a *API) trendLiveSearchTop(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	top, err := a.live.SearchTop(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, top)
}

func (a *API) getOpenAlexBudget(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, map[string]any{"budgets": a.live.OpenAlexBudgets(r.Context())})
}

func (a *API) trendLiveSearches(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	history, err := a.live.SearchHistory(r.Context(), limit)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "live search history unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, history)
}

// trendLiveExecutive — запись, а не чтение: ненаписанная справка стоит минуты работы модели.
func (a *API) trendLiveExecutive(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	// Уход со страницы не отменяет вызов модели: справка ляжет в кэш. Срок ограничивает писатель.
	brief, err := a.live.Executive(context.WithoutCancel(r.Context()), r.URL.Query().Get("q"),
		r.URL.Query().Get("topic"))
	switch {
	case errors.Is(err, application.ErrLiveAnswerNotFound):
		httpx.Error(w, http.StatusNotFound, "saved answer not found")
		return
	case errors.Is(err, application.ErrLLMNotConfigured):
		httpx.Error(w, http.StatusServiceUnavailable, "llm gateway not configured")
		return
	case err != nil:
		slog.Warn("live executive brief failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "executive brief failed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, brief)
}
