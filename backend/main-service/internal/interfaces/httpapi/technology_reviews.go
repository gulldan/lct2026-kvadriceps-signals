package httpapi

import (
	"errors"
	"io"
	"net/http"
	"uuid"

	"encoding/json/v2"

	"github.com/example/main-service/internal/platform/extractdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

type reviewView struct {
	Operation     string         `json:"operation"`
	Reason        string         `json:"reason"`
	PreviousValue map[string]any `json:"previous_value"`
	NewValue      map[string]any `json:"new_value"`
	RevertsID     string         `json:"reverts_id,omitempty"`
}

// submitReview пишет правку версией с заменённым значением, чтобы её можно было откатить; опубликованные
// снимки не переписываются, а повтор той же правки возвращает уже записанную версию.
func (a *API) submitReview(w http.ResponseWriter, r *http.Request) {
	author, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.technologies == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "technology catalogue unavailable")
		return
	}
	var body reviewView
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || json.Unmarshal(raw, &body, json.RejectUnknownMembers(true)) != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid review")
		return
	}
	stored, err := a.technologies.Review(r.Context(), extractdoc.Review{
		ID: uuid.NewV4().String(), TechnologyID: r.PathValue("id"), AuthorID: author,
		Operation: body.Operation, Reason: body.Reason, PreviousValue: body.PreviousValue,
		NewValue: body.NewValue, RevertsID: body.RevertsID,
	})
	if err != nil {
		a.reviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusCreated, stored)
}

func (a *API) readReviews(w http.ResponseWriter, r *http.Request) {
	reader, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.technologies == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "technology catalogue unavailable")
		return
	}
	history, err := a.technologies.Reviews(r.Context(), extractdoc.ReadReviews{
		ReaderID: reader, TechnologyID: r.PathValue("id"), SharedCorpus: a.sharedCorpus,
	})
	if err != nil {
		a.reviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"technology_id": r.PathValue("id"), "reviews": history})
}

func (a *API) reviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, extractdoc.ErrInvalid):
		httpx.Error(w, http.StatusBadRequest, "a correction needs an operation, a reason and the value it replaced")
	case errors.Is(err, extractdoc.ErrNotFound):
		httpx.Error(w, http.StatusForbidden, "technology not available to this account")
	case errors.Is(err, extractdoc.ErrConflict):
		httpx.Error(w, http.StatusConflict, "technology changed; reload it before correcting")
	default:
		httpx.Error(w, http.StatusInternalServerError, "technology review failed")
	}
}
