package httpapi

import (
	"net/http"
	"strings"

	"github.com/example/main-service/internal/platform/httpx"
)

type kpiParseRequest struct {
	Prompt string `json:"prompt"`
}

func (a *API) parseKPIPrompt(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var req kpiParseRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		httpx.Error(w, http.StatusBadRequest, "prompt is required")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"kpi": a.hypotheses.ParseKPIPrompt(r.Context(), ownerID, req.Prompt),
	})
}
