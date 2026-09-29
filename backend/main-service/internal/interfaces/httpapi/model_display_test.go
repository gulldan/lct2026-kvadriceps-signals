package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/httpx"
)

func TestModelDisplayNamesHideGatewayID(t *testing.T) {
	const id = "corp/deepseek-v4-flash-abliterated"
	t.Setenv("VLLM_MODEL", id)
	api := &API{}
	handler := api.ModelDisplayNames(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trends/analyses/x/export" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			_, _ = w.Write([]byte(id))
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"card":  map[string]string{"model": id, "verification_model": id},
			"title": "Uncensored " + id + " fine-tune",
		})
	}))
	serve := func(path string) string {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		return rec.Body.String()
	}
	body := serve("/trends/live/search?q=x")
	if strings.Count(body, `"`+modelDisplayDefault+`"`) != 2 || !strings.Contains(body, "Uncensored "+id) {
		t.Fatalf("model fields are not renamed or document text changed: %s", body)
	}
	if !strings.Contains(serve("/trends/analyses/x/export"), id) {
		t.Error("CSV export must keep the raw id")
	}
	if got := displayModelName([]byte(`{"model":"local/deepseek-v4-flash-abliterated"}`), id, "X"); string(got) != `{"model":"X"}` {
		t.Errorf("provider prefix must not keep the raw id: %s", got)
	}
}
