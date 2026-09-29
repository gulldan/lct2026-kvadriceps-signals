package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

// topicMetrics читает профиль тем же контрактом v1, что и экспорт; нет темы — 404, а не пустое измерение.
func (a *API) topicMetrics(w http.ResponseWriter, r *http.Request) {
	job, ok := a.loadAnalysis(w, r)
	if !ok {
		return
	}
	if job.Status != analysisdoc.Published {
		httpx.Error(w, http.StatusConflict, "analysis not published")
		return
	}
	if job.Request == nil {
		a.analysisError(w, analysisdoc.ErrInvalid)
		return
	}
	profiles, limitations, err := exportProfiles(job)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	for _, profile := range profiles {
		if profile.id != r.PathValue("topic") {
			continue
		}
		analysisJSON(w, http.StatusOK, map[string]any{
			fieldAnalysisID: job.ID, "analysis_run_id": job.ID, "schema_version": analysisdoc.ResultVersion,
			fieldMethodHash: job.MethodSHA256, "topic": profile.id, "title": profile.title,
			"history_mode": analysisdoc.NormalizeMode(job.Request.Mode), "cutoff": job.Request.Cutoff,
			"metrics": profile.measurements, csvLimitations: limitations,
		})
		return
	}
	httpx.Error(w, http.StatusNotFound, "topic unavailable in this analysis")
}
