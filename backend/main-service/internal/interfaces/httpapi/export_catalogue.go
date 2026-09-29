package httpapi

import (
	"cmp"
	"context"
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/extractdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

const catalogueExportPage = 100

// exportTechnologies идёт через тот же поиск, что и экран, чтобы файл и права доступа совпадали; пересчёт
// каталога посреди выгрузки завершает файл видимым ограничением, а не сшивает два каталога.
// ponytail: поток постранично без очереди выгрузок; потолок — таймаут прокси на один HTTP-ответ.
func (a *API) exportTechnologies(w http.ResponseWriter, r *http.Request) {
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
	format := cmp.Or(q.Get("format"), formatJSON)
	if format != formatJSON && format != formatCSV {
		httpx.Error(w, http.StatusBadRequest, "format must be json or csv")
		return
	}
	search := extractdoc.SearchTechnologies{
		ReaderID: reader, Query: q.Get("q"), Limit: catalogueExportPage,
		Languages: values(q, "language"), Roles: values(q, "role"),
		IncludeAll: q.Get("q") == "", SharedCorpus: a.sharedCorpus,
	}
	// Первая страница читается до заголовков: отказ запроса — это статус, а не пустой файл.
	first, err := a.technologies.Search(r.Context(), search)
	switch {
	case errors.Is(err, extractdoc.ErrInvalid):
		httpx.Error(w, http.StatusBadRequest,
			strings.TrimPrefix(err.Error(), extractdoc.ErrInvalid.Error()+": "))
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "technology catalogue failed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="technologies.`+format+`"`)
	if format == formatCSV {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		catalogueCSV(r.Context(), w, a.technologies, search, first)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	catalogueJSON(r.Context(), w, a.technologies, search, first)
}

// cataloguePages возвращает ограничения файла — причину пустого каталога или сбой, оборвавший выгрузку:
// короткий файл всегда говорит, что он короткий.
func cataloguePages(ctx context.Context, technologies application.Technologies, search extractdoc.SearchTechnologies,
	page extractdoc.TechnologyPage, emit func(extractdoc.TechnologyHit) error) []string {
	limitations := []string{}
	if page.Reason != "" {
		limitations = append(limitations, page.Reason)
	}
	for {
		for _, hit := range page.Technologies {
			if err := emit(hit); err != nil {
				return limitations
			}
		}
		if page.NextCursor == "" {
			return limitations
		}
		search.Cursor = page.NextCursor
		var err error
		if page, err = technologies.Search(ctx, search); err != nil {
			reason := "catalogue failed"
			if errors.Is(err, extractdoc.ErrConflict) {
				reason = "catalogue changed during export"
			}
			return append(limitations, "export_incomplete: "+reason)
		}
	}
}

func catalogueCSV(ctx context.Context, w io.Writer, technologies application.Technologies,
	search extractdoc.SearchTechnologies, first extractdoc.TechnologyPage) {
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"technology_id", "canonical_name", "validation_status", "description", "match_reasons"})
	limitations := cataloguePages(ctx, technologies, search, first, func(hit extractdoc.TechnologyHit) error {
		return writer.Write([]string{hit.TechnologyID, hit.CanonicalName, hit.ValidationStatus, hit.Description,
			strings.Join(hit.MatchReasons, " ")})
	})
	if len(limitations) > 0 {
		_ = writer.Write(nil)
		_ = writer.Write([]string{csvLimitations})
		for _, limitation := range limitations {
			_ = writer.Write([]string{limitation})
		}
	}
	writer.Flush()
}

func catalogueJSON(ctx context.Context, w io.Writer, technologies application.Technologies,
	search extractdoc.SearchTechnologies, first extractdoc.TechnologyPage) {
	head, _ := json.Marshal(struct {
		SchemaVersion    string `json:"schema_version"`
		CatalogueVersion string `json:"catalogue_version"`
	}{first.SchemaVersion, first.CatalogueVersion})
	_, _ = w.Write(append(head[:len(head)-1], `,"technologies":[`...))
	count := 0
	limitations := cataloguePages(ctx, technologies, search, first, func(hit extractdoc.TechnologyHit) error {
		raw, err := json.Marshal(hit)
		if err != nil {
			return fmt.Errorf("encode technology: %w", err)
		}
		if count > 0 {
			raw = append([]byte{','}, raw...)
		}
		count++
		if _, err = w.Write(raw); err != nil {
			return fmt.Errorf("write technology: %w", err)
		}
		return nil
	})
	tail, _ := json.Marshal(limitations)
	_, _ = w.Write(append(append([]byte(`],"limitations":`), tail...), '}'))
}
