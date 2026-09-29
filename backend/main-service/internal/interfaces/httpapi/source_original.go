package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (a *API) sourceOriginal(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	raw, snapshot, err := a.ingestion.SourceOriginal(r.Context(), claims.UserID,
		r.PathValue("id"), r.URL.Query().Get("snapshot"))
	if err != nil {
		sourceReadError(w, err)
		return
	}
	contentType := snapshot.MIMEType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	disposition := "attachment"
	if contentType == "application/pdf" || strings.HasPrefix(contentType, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": snapshot.Filename}))
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, bytes.NewReader(raw))
}

func sourceReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrNotFound), errors.Is(err, application.ErrForbidden),
		errors.Is(err, dbclient.ErrNotFound), errors.Is(err, storage.ErrNotFound),
		status.Code(err) == codes.NotFound, status.Code(err) == codes.PermissionDenied:
		httpx.Error(w, http.StatusNotFound, "saved source unavailable")
	case errors.Is(err, application.ErrInvalidArgument), status.Code(err) == codes.InvalidArgument:
		httpx.Error(w, http.StatusBadRequest, "invalid source or snapshot")
	default:
		httpx.Error(w, http.StatusServiceUnavailable, "saved source temporarily unavailable")
	}
}
