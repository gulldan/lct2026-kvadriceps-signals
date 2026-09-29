package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/storage"
)

func (a *API) listDocuments(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if r.URL.RawQuery == "" {
		a.listAllDocuments(w, r, ownerID)
		return
	}
	page, err := a.ingestion.DocumentPage(r.Context(), documentQuery(r, ownerID, false))
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.FormatInt(page.GetTotal(), 10))
	w.Header().Set("X-Next-Cursor", page.GetNextCursor())
	httpx.JSON(w, http.StatusOK, newDocumentViews(page.GetDocuments()))
}

func (a *API) getDocument(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	doc, err := a.ingestion.GetDocument(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newDocumentView(doc))
}

// documentChunks: операторы и админы читают любой документ, пользователи — только свои.
func (a *API) documentChunks(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if snapshot := r.URL.Query().Get("snapshot"); snapshot != "" {
		a.historicalSourceChunks(w, r, claims.UserID, snapshot)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	privileged := a.sharedCorpus || hasAnyRole(claims, "operator", "admin")
	doc, chunks, err := a.ingestion.DocumentChunks(r.Context(), claims.UserID, privileged, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newDocumentChunksView(doc, chunks))
}

// documentContent проверяет доступ так же, как documentChunks (владелец или общий корпус).
func (a *API) documentContent(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	privileged := a.sharedCorpus || hasAnyRole(claims, "operator", "admin")
	rc, doc, err := a.ingestion.OpenDocument(r.Context(), claims.UserID, privileged, r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, "original file is not available")
		case errors.Is(err, application.ErrForbidden), errors.Is(err, application.ErrNotFound), errors.Is(err, dbclient.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, "not found")
		default:
			// Внутренности хранилища и БД браузеру не отдаются.
			httpx.Error(w, http.StatusInternalServerError, "failed to load document content")
		}
		return
	}
	defer func() { _ = rc.Close() }()
	ct := doc.GetMimeType()
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", doc.GetFilename()))
	if strings.HasPrefix(ct, "text/html") {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", doc.GetFilename()))
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	if doc.GetSourceMetadata() != "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, rc)
}
