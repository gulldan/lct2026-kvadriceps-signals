package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"encoding/json/v2"
	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/harvest"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) Collections(client application.Collections) { a.collections = client }

type collectionRequestView struct {
	RequestID string        `json:"request_id"`
	Provider  string        `json:"provider"`
	Scope     harvest.Scope `json:"scope"`
	URLs      []string      `json:"urls,omitempty"`
	Limit     int           `json:"limit"`
}

// requestCollection отвечает сразу: сбор идёт в сборщике минутами, и ожидание дало бы таймаут, а не ответ.
func (a *API) requestCollection(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.collections == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "collection unavailable")
		return
	}
	var body collectionRequestView
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || json.Unmarshal(raw, &body, json.RejectUnknownMembers(true)) != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid collection request")
		return
	}
	collection, err := a.collections.Request(r.Context(), harvest.Request{
		OwnerID: owner, RequestID: body.RequestID, Provider: body.Provider,
		Scope: body.Scope, URLs: body.URLs, Limit: body.Limit,
	})
	if err != nil {
		a.collectionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/api/v1/trends/collections/"+collection.ScopeID)
	code := http.StatusAccepted
	if collection.State == harvest.StateComplete || collection.State == harvest.StateFailed {
		code = http.StatusOK
	}
	httpx.JSON(w, code, collection)
}

func (a *API) readCollection(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.collections == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "collection unavailable")
		return
	}
	collection, err := a.collections.Progress(r.Context(),
		harvest.Progress{OwnerID: owner, ScopeID: r.PathValue("id")})
	if err != nil {
		a.collectionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, collection)
}

func (a *API) collectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, harvest.ErrInvalid):
		// Отказ периметра называет хост: оператору надо знать, какой именно адрес вне панели.
		message := "invalid collection request"
		if detail := strings.TrimPrefix(err.Error(), harvest.ErrInvalid.Error()+": "); strings.Contains(
			detail, "outside the configured domains") {
			message = detail
		}
		httpx.Error(w, http.StatusBadRequest, message)
	case errors.Is(err, harvest.ErrConflict):
		httpx.Error(w, http.StatusConflict, "request_id already names a different collection")
	case errors.Is(err, harvest.ErrNotFound):
		httpx.Error(w, http.StatusForbidden, "collection not available to this account")
	default:
		httpx.Error(w, http.StatusInternalServerError, "collection control plane failed")
	}
}
