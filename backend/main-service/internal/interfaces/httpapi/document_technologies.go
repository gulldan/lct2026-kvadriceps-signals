package httpapi

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/extractdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) SetTechnologies(client application.Technologies) { a.technologies = client }

// documentTechnologies: пустой список читается только вместе с coverage.status — no_technology (в ревизии
// технологий нет), failed (попытка сломалась), not_extracted (ещё не смотрели).
func (a *API) documentTechnologies(w http.ResponseWriter, r *http.Request) {
	reader, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.technologies == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "technology catalogue unavailable")
		return
	}
	out, err := a.technologies.ReadRevision(r.Context(), extractdoc.ReadRevision{
		ReaderID: reader, SourceRevisionID: r.PathValue("id"),
		ExtractorVersion: r.URL.Query().Get("extractor_version"),
		SchemaVersion:    r.URL.Query().Get("schema_version"),
		SharedCorpus:     a.sharedCorpus,
	})
	switch {
	case errors.Is(err, extractdoc.ErrInvalid):
		httpx.Error(w, http.StatusBadRequest, "invalid revision request")
		return
	case errors.Is(err, extractdoc.ErrNotFound):
		// Недоступная ревизия отвечает так же, как несуществующая: пустой список подтвердил бы, что она есть.
		httpx.Error(w, http.StatusForbidden, "revision not available to this account")
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "technology read failed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

// readTechnology считает только читаемую часть, чтобы приватный документ не двигал чужие цифры.
func (a *API) readTechnology(w http.ResponseWriter, r *http.Request) {
	reader, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.technologies == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "technology catalogue unavailable")
		return
	}
	out, err := a.technologies.ReadTechnology(r.Context(), extractdoc.ReadTechnology{
		ReaderID: reader, TechnologyID: r.PathValue("id"), SharedCorpus: a.sharedCorpus,
	})
	if err != nil {
		a.reviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) searchTechnologies(w http.ResponseWriter, r *http.Request) {
	reader, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.technologies == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "technology catalogue unavailable")
		return
	}
	q := r.URL.Query()
	limit, err := strconv.Atoi(cmp.Or(q.Get("limit"), "20"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "limit must be a number between 1 and 100")
		return
	}
	page, err := a.technologies.Search(r.Context(), extractdoc.SearchTechnologies{
		ReaderID: reader, Query: q.Get("q"), Limit: limit, Cursor: q.Get("cursor"),
		Languages: values(q, "language"), Roles: values(q, "role"), Stages: values(q, "stage"),
		IncludeAll: q.Get("all") == "true", SharedCorpus: a.sharedCorpus,
	})
	switch {
	case errors.Is(err, extractdoc.ErrConflict):
		// Каталог изменился под курсором: продолжение сшило бы страницы разных каталогов.
		httpx.Error(w, http.StatusConflict, "catalogue changed; restart the search")
		return
	case errors.Is(err, extractdoc.ErrInvalid):
		// Причина отказа отдаётся как есть: неподдерживаемый фильтр и кривой размер страницы клиент должен различать.
		httpx.Error(w, http.StatusBadRequest,
			strings.TrimPrefix(err.Error(), extractdoc.ErrInvalid.Error()+": "))
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "technology catalogue failed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, page)
}

// values принимает и повторяющийся параметр, и одно значение через запятую — так шлёт форма браузера.
func values(q url.Values, name string) []string {
	out := []string{}
	for _, raw := range q[name] {
		for part := range strings.SplitSeq(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
