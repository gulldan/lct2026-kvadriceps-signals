package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) suggestKPIs(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if !a.ovr.GetBool(r.Context(), "KPI_SUGGEST", true) {
		httpx.JSON(w, http.StatusOK, map[string]any{"suggestions": []application.KPISuggestion{}})
		return
	}
	suggestions, err := a.hypotheses.SuggestKPIs(r.Context(), ownerID)
	if err != nil || suggestions == nil {
		// Недоступная LLM не должна ронять страницу в 500.
		suggestions = []application.KPISuggestion{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"suggestions": suggestions})
}
