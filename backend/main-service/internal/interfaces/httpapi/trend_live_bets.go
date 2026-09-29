package httpapi

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

// liveSignalBriefBody — карточка кандидата весит десятки килобайт вместе с
// цитатами и событиями; четыре мегабайта — с запасом на самый полный ответ.
const liveSignalBriefBody = 4 << 20

func (a *API) SetSignalBrief(write application.LiveSignalBriefWriter) { a.signalBrief = write }

// trendLiveSignalBrief берёт карточку от клиента: выдача могла ещё не лечь в базу, а черновик правит
// и сохраняет от своего имени тот же человек.
func (a *API) trendLiveSignalBrief(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.signalBrief == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "signal brief unavailable")
		return
	}
	var in application.LiveSignalBriefInput
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, liveSignalBriefBody), &in); err != nil ||
		in.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid signal brief request")
		return
	}
	// Закрытое окно не отменяет вызов модели: срок ограничивает писатель.
	if a.live != nil {
		defer a.live.Interactive()()
	}
	w.Header().Set("Cache-Control", "no-store")
	analysisJSON(w, http.StatusOK, a.signalBrief(context.WithoutCancel(r.Context()), in))
}

func (a *API) trendLiveAffinity(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	var in struct {
		Text    string   `json:"text"`
		Options []string `json:"options"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid affinity request")
		return
	}
	similarity, err := a.live.Affinity(r.Context(), in.Text, in.Options)
	switch {
	case errors.Is(err, application.ErrLiveAffinityInvalid):
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		slog.Warn("live affinity failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "affinity unavailable")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"similarity": similarity, "floor": application.LiveAffinityFloor})
}

// trendLiveBetsCheck: владелец сверяет свой прогноз (hypothesis) или все свои, администратор — все.
func (a *API) trendLiveBetsCheck(w http.ResponseWriter, r *http.Request) {
	owner, ok := a.liveWatchOwner(w, r)
	if !ok {
		return
	}
	if a.hypothesesPrivileged(r) {
		owner = ""
	}
	report, err := a.watches.CheckBets(r.Context(), owner, r.URL.Query().Get("hypothesis"), true)
	switch {
	case errors.Is(err, application.ErrLiveBetsOff):
		httpx.Error(w, http.StatusServiceUnavailable, "bet tracking disabled")
		return
	case err != nil:
		slog.Warn("live bets check failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "bet check failed")
		return
	}
	analysisJSON(w, http.StatusOK, report)
}
