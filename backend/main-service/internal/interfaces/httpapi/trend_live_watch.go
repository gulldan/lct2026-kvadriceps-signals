package httpapi

import (
	"errors"
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) SetLiveWatches(watches *application.LiveWatches) { a.watches = watches }

type liveWatchRequest struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Title  string `json:"title,omitempty"`
}

// liveWatchOwner — владелец подписок или отказ, уже записанный в ответ.
func (a *API) liveWatchOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return "", false
	}
	if a.watches == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live watches disabled")
		return "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	return owner, true
}

func liveWatchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrLiveWatchInvalid):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, application.ErrLiveWatchNotFound):
		httpx.Error(w, http.StatusNotFound, "live watch not found")
	default:
		httpx.Error(w, http.StatusBadGateway, "live watches unavailable")
	}
}

func (a *API) liveWatchList(w http.ResponseWriter, r *http.Request) {
	owner, ok := a.liveWatchOwner(w, r)
	if !ok {
		return
	}
	watches, err := a.watches.List(r.Context(), owner)
	if err != nil {
		liveWatchError(w, err)
		return
	}
	analysisJSON(w, http.StatusOK, map[string][]application.LiveWatch{"watches": watches})
}

func (a *API) liveWatchCreate(w http.ResponseWriter, r *http.Request) {
	owner, ok := a.liveWatchOwner(w, r)
	if !ok {
		return
	}
	var req liveWatchRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid watch request")
		return
	}
	watch, err := a.watches.Watch(r.Context(), owner, req.Kind, req.Target, req.Title)
	if err != nil {
		liveWatchError(w, err)
		return
	}
	analysisJSON(w, http.StatusOK, watch)
}

// liveWatchDelete берёт предмет подписки из адреса: тело у DELETE прокси
// вправе отбросить.
func (a *API) liveWatchDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := a.liveWatchOwner(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	if err := a.watches.Unwatch(r.Context(), owner, query.Get("kind"), query.Get("target")); err != nil {
		liveWatchError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) liveWatchAck(w http.ResponseWriter, r *http.Request) {
	owner, ok := a.liveWatchOwner(w, r)
	if !ok {
		return
	}
	var req liveWatchRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid watch request")
		return
	}
	if err := a.watches.Ack(r.Context(), owner, req.Kind, req.Target); err != nil {
		liveWatchError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
