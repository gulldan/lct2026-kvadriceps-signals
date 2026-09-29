package httpapi

import (
	"io"
	"net/http"
	"strings"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) analysisDescription(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.analyses == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "analysis unavailable")
		return
	}
	start := r.Method == http.MethodPost
	if start {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2))
		if err != nil || (len(raw) != 0 && strings.TrimSpace(string(raw)) != "{}") {
			httpx.Error(w, http.StatusBadRequest, "description takes no client facts")
			return
		}
	}
	d, err := a.analyses.Description(r.Context(), analysisdoc.DescriptionRead{JobID: r.PathValue("id"),
		ReaderID: owner, SharedCorpus: a.sharedCorpus, CandidateID: r.PathValue("candidate")}, start)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	code := http.StatusOK
	if d.Status == "prepared" {
		code = http.StatusAccepted
	}
	analysisJSON(w, code, d)
}
