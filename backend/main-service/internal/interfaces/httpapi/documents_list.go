package httpapi

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/main-service/internal/platform/contracts"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

// Старые фоновые потребители читают полный массив без параметров и не знают о курсоре.
// Канонические страницы сохраняют права доступа, а поток не держит весь корпус в памяти API.
func (a *API) listAllDocuments(w http.ResponseWriter, r *http.Request, owner string) {
	query := contracts.DocumentQuery{ReaderID: owner, Limit: 500}
	page, err := a.ingestion.DocumentPage(r.Context(), query)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Total-Count", strconv.FormatInt(page.GetTotal(), 10))
	if err = a.writeDocumentPages(r.Context(), w, query, page); err != nil {
		// Незавершённый массив нельзя принимать за полный: потребитель должен повторить запрос.
		slog.ErrorContext(r.Context(), "document list interrupted", "error", err)
	}
}

func (a *API) writeDocumentPages(ctx context.Context, w io.Writer,
	query contracts.DocumentQuery, page *dbv1.DocumentPageResponse,
) error {
	out := jsontext.NewEncoder(w)
	if err := out.WriteToken(jsontext.BeginArray); err != nil {
		return fmt.Errorf("start document list: %w", err)
	}
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("document list canceled: %w", err)
		}
		for _, doc := range page.GetDocuments() {
			if err := json.MarshalEncode(out, newDocumentView(doc), jsonv1.DefaultOptionsV1()); err != nil {
				return fmt.Errorf("write document: %w", err)
			}
		}
		cursor := page.GetNextCursor()
		if cursor == "" {
			break
		}
		if seen[cursor] {
			return errors.New("document list cursor did not advance")
		}
		seen[cursor] = true
		query.BeforeID = cursor
		var err error
		page, err = a.ingestion.DocumentPage(ctx, query)
		if err != nil {
			return fmt.Errorf("read document page: %w", err)
		}
		if len(page.GetDocuments()) == 0 {
			return errors.New("document list continuation disappeared")
		}
	}
	if err := out.WriteToken(jsontext.EndArray); err != nil {
		return fmt.Errorf("finish document list: %w", err)
	}
	return nil
}
