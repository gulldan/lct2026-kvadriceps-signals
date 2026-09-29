package httpapi

import (
	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
	"net/http"
	"strings"
	"time"
)

type adminDocumentListView struct {
	Items      []adminDocumentView `json:"items"`
	Total      int                 `json:"total"`
	Limit      int                 `json:"limit"`
	Offset     int                 `json:"offset"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func documentQuery(r *http.Request, reader string, all bool) contracts.DocumentQuery {
	limit := queryInt(r, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	return contracts.DocumentQuery{ReaderID: reader, AllOwners: all, Query: r.URL.Query().Get(
		"q",
	), Status: r.URL.Query().Get(
		"status",
	),
		FileType: r.URL.Query().Get("type"), BeforeID: r.URL.Query().Get("before"), Limit: min(limit, 500),
		Offset: max(queryInt(r, "offset", 0), 0)}
}
func (a *API) adminDocuments(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	claims, _ := jwt.ClaimsFromContext(r.Context())
	q := documentQuery(r, claims.UserID, true)
	page, err := a.ingestion.DocumentPage(r.Context(), q)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, adminDocumentListView{Items: newAdminDocumentViews(
		page.GetDocuments(),
	), Total: int(
		page.GetTotal(),
	),
		Limit: q.Limit, Offset: q.Offset, NextCursor: page.GetNextCursor()})
}
func (a *API) adminDocumentsStats(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	claims, _ := jwt.ClaimsFromContext(r.Context())
	stats, err := a.ingestion.DocumentStats(r.Context(), documentQuery(r, claims.UserID, true))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, stats)
}
func (a *API) requeueDocuments(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "queued"
	}
	olderMin := queryInt(r, "older_min", 10)
	limit := queryInt(r, "limit", 5000)
	var n int
	var err error
	if ids := strings.Fields(strings.ReplaceAll(r.URL.Query().Get("ids"), ",", " ")); len(ids) > 0 {
		n, err = a.ingestion.RequeueDocuments(r.Context(), ids)
	} else {
		n, err = a.ingestion.RequeueStuck(r.Context(), status, time.Duration(olderMin)*time.Minute, limit)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"requeued": n})
}
