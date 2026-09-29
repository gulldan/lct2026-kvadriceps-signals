package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) trendStudy(w http.ResponseWriter, r *http.Request) {
	reader, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	study, err := a.ingestion.TrendStudy(r.Context(), reader)
	if err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, "research corpus temporarily unavailable")
		return
	}
	analysisJSON(w, http.StatusOK, study)
}
