package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

// trendLiveCover отвечает 204, если рисунка нет: для карточки это не ошибка, блок просто не рисуется.
func (a *API) trendLiveCover(w http.ResponseWriter, r *http.Request) {
	a.writeLiveCover(w, r, "/trends/live/topics/", a.liveCover(w, r, a.covers.Cover))
}

func (a *API) trendLivePaperCover(w http.ResponseWriter, r *http.Request) {
	a.writeLiveCover(w, r, "/trends/live/papers/", a.liveCover(w, r, a.covers.PaperCover))
}

func (a *API) writeLiveCover(w http.ResponseWriter, r *http.Request, prefix string, cover *application.LiveCover) {
	if cover == nil {
		return
	}
	cover.ImageURL = prefix + url.PathEscape(r.PathValue("id")) + "/cover/image"
	w.Header().Set("Cache-Control", "private, max-age=3600")
	analysisJSON(w, http.StatusOK, cover)
}

// trendLiveCoverImage отдаёт байты, а не ссылку на arXiv: из корпоративной сети он может быть не виден,
// а прямая ссылка сообщила бы третьей стороне адрес читателя. Адрес клиент не передаёт вовсе —
// картинка берётся только из найденной сервером.
func (a *API) trendLiveCoverImage(w http.ResponseWriter, r *http.Request) {
	writeLiveCoverImage(w, a.liveCover(w, r, a.covers.Cover))
}

func (a *API) trendLivePaperCoverImage(w http.ResponseWriter, r *http.Request) {
	writeLiveCoverImage(w, a.liveCover(w, r, a.covers.PaperCover))
}

func writeLiveCoverImage(w http.ResponseWriter, cover *application.LiveCover) {
	if cover == nil {
		return
	}
	w.Header().Set("Content-Type", cover.MIME)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = io.Copy(w, bytes.NewReader(cover.Image))
}

// liveCover при nil уже записал ответ сам.
func (a *API) liveCover(w http.ResponseWriter, r *http.Request,
	find func(context.Context, string) (application.LiveCover, error)) *application.LiveCover {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return nil
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return nil
	}
	cover, err := find(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, application.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "cover source not found")
		return nil
	case cover.Image == nil:
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return &cover
}
