package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) createHypothesisJob(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.hypJobs == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "hypothesis job service is not configured")
		return
	}
	var in hypothesisJobInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.HypothesisID != "" {
		if ownerID, ok = a.actOwnerFor(w, r, in.HypothesisID); !ok {
			return
		}
	}
	job, err := a.hypJobs.Enqueue(r.Context(), ownerID, in.Kind, in.toApplicationInput())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, newHypothesisJobView(job))
}

func (a *API) listHypothesisJobs(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.hypJobs == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "hypothesis job service is not configured")
		return
	}
	jobs, err := a.hypJobs.List(r.Context(), ownerID, queryInt(r, "limit", 50))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newHypothesisJobViews(jobs))
}

func (a *API) getHypothesisJob(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.hypJobs == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "hypothesis job service is not configured")
		return
	}
	job, err := a.hypJobs.Get(r.Context(), ownerID, r.PathValue("id"))
	if err != nil && a.hypothesesPrivileged(r) {
		job, err = a.hypJobs.GetAny(r.Context(), r.PathValue("id"))
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newHypothesisJobView(job))
}

type hypothesisJobInput struct {
	RequestID      string   `json:"request_id"`
	Kind           string   `json:"kind"`
	HypothesisID   string   `json:"hypothesis_id"`
	KPIID          string   `json:"kpi_id"`
	KPITitle       string   `json:"kpi_title"`
	KPIMetric      string   `json:"kpi_metric"`
	KPIDescription string   `json:"kpi_description"`
	Constraints    string   `json:"constraints"`
	Count          int      `json:"count"`
	DocumentIDs    []string `json:"document_ids"`
}

func (in hypothesisJobInput) toApplicationInput() application.HypothesisJobInput {
	return application.HypothesisJobInput{
		RequestID:      in.RequestID,
		HypothesisID:   in.HypothesisID,
		KPIID:          in.KPIID,
		KPITitle:       in.KPITitle,
		KPIMetric:      in.KPIMetric,
		KPIDescription: in.KPIDescription,
		Constraints:    in.Constraints,
		Count:          in.Count,
		DocumentIDs:    in.DocumentIDs,
	}
}

type hypothesisJobView struct {
	ID          string                         `json:"id"`
	OwnerID     string                         `json:"owner_id"`
	Kind        string                         `json:"kind"`
	Status      string                         `json:"status"`
	Input       application.HypothesisJobInput `json:"input"`
	ResultIDs   []string                       `json:"result_ids"`
	Error       string                         `json:"error,omitempty"`
	CreatedAt   string                         `json:"created_at"`
	StartedAt   string                         `json:"started_at,omitempty"`
	HeartbeatAt string                         `json:"heartbeat_at,omitempty"`
	FinishedAt  string                         `json:"finished_at,omitempty"`
}

func newHypothesisJobView(j *application.HypothesisJob) hypothesisJobView {
	return hypothesisJobView{
		ID: j.ID, OwnerID: j.OwnerID, Kind: j.Kind, Status: j.Status, Input: j.Input,
		ResultIDs: orEmptyStrings(j.ResultIDs), Error: j.Error, CreatedAt: j.CreatedAt, StartedAt: j.StartedAt,
		HeartbeatAt: j.HeartbeatAt, FinishedAt: j.FinishedAt,
	}
}

func newHypothesisJobViews(jobs []*application.HypothesisJob) []hypothesisJobView {
	out := make([]hypothesisJobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, newHypothesisJobView(j))
	}
	return out
}
