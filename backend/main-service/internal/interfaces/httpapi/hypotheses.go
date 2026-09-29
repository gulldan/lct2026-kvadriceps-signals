package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

// decodeResource, в отличие от httpx.Decode, терпит неизвестные и серверные поля, чтобы клиент
// мог вернуть через PUT то, что получил через GET.
func decodeResource(r *http.Request, v any) error {
	return decodeResourceLimited(r, v, 1<<20)
}

// decodeResourceLimited нужен массовым ручкам вроде /clusters/replace: вся доска не влезает в 1 МиБ.
func decodeResourceLimited(r *http.Request, v any, maxBytes int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

// Ручки фабрики гипотез без проверки роли; владелец всегда из проверенного JWT, не из тела запроса.

func (a *API) updateOwned[I, P, V any](
	w http.ResponseWriter, r *http.Request,
	apply func(ctx context.Context, ownerID, id string, in I) error,
	get func(ctx context.Context, ownerID, id string) (P, error),
	view func(P) V,
) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in I
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	if err := apply(r.Context(), ownerID, id, in); err != nil {
		a.fail(w, err)
		return
	}
	out, err := get(r.Context(), ownerID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view(out))
}

// hypothesisActOwner для админа возвращает настоящего владельца записи, чтобы нижележащие вызовы
// с проверкой владельца сошлись.
func (a *API) hypothesisActOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	return a.actOwnerFor(w, r, r.PathValue("id"))
}

func (a *API) actOwnerFor(w http.ResponseWriter, r *http.Request, hypothesisID string) (string, bool) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return "", false
	}
	if !a.hypothesesPrivileged(r) {
		return ownerID, true
	}
	h, err := a.hypotheses.GetHypothesis(r.Context(), ownerID, true, hypothesisID)
	if err != nil {
		a.fail(w, err)
		return "", false
	}
	return h.GetOwnerId(), true
}

func (a *API) listHypotheses(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	q := r.URL.Query()
	req := hypothesisFilters(r)
	req.Limit = int32(min(max(queryInt(r, "limit", 100), 1), 500))
	req.Compact = q.Get("view") == "ref"
	req.Offset = int32(min(max(queryInt(r, "offset", 0), 0), 100000))
	req.DocumentIds = splitIDList(q.Get("document_ids"))
	hs, err := a.hypotheses.ListHypotheses(r.Context(), ownerID, a.hypothesesPrivileged(r), req)
	if err != nil {
		a.fail(w, err)
		return
	}
	if q.Get("view") == "ref" {
		httpx.JSON(w, http.StatusOK, newHypothesisRefViews(hs))
		return
	}
	httpx.JSON(w, http.StatusOK, newHypothesisViews(hs))
}

// hypothesisFilters: пагинация и document_ids — только для списка; доска листает после своих
// фильтров, чтобы агрегаты оставались полными.
func hypothesisFilters(r *http.Request) *dbv1.ListHypothesesRequest {
	q := r.URL.Query()
	req := &dbv1.ListHypothesesRequest{
		Status: q.Get("status"), KpiId: q.Get("kpi_id"), ClusterId: q.Get("cluster_id"),
		FunctionArea: q.Get("function_area"), SourceType: q.Get("source_type"), Organization: q.Get("organization"),
		MinTrl: int32(queryInt(r, "min_trl", 0)), MaxTrl: int32(queryInt(r, "max_trl", 0)),
		OrderBy: q.Get("order_by"),
	}
	if tags := strings.TrimSpace(q.Get("tags")); tags != "" {
		req.Tags = strings.Split(tags, ",")
	}
	return req
}

func (a *API) createHypothesis(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in hypothesisInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Title == "" || in.Statement == "" {
		httpx.Error(w, http.StatusBadRequest, "title and statement are required")
		return
	}
	req := &dbv1.CreateHypothesisRequest{
		Hypothesis: in.toProto(""),
		Initial:    in.Revision.toProto(""),
	}
	h, err := a.hypotheses.CreateHypothesis(r.Context(), ownerID, req)
	if err != nil {
		a.fail(w, err)
		return
	}
	if a.hypJobs != nil {
		a.hypJobs.EnsureVerify(r.Context(), ownerID, []*commonv1.Hypothesis{h}, 1)
		a.hypJobs.EnsureAssessTRL(r.Context(), ownerID, []*commonv1.Hypothesis{h}, 1)
	}
	httpx.JSON(w, http.StatusCreated, newHypothesisView(h))
}

func (a *API) generateHypotheses(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in struct {
		KPIID          string   `json:"kpi_id"`
		KPITitle       string   `json:"kpi_title"`
		KPIMetric      string   `json:"kpi_metric"`
		KPIDescription string   `json:"kpi_description"`
		Constraints    string   `json:"constraints"`
		Count          int      `json:"count"`
		DocumentIDs    []string `json:"document_ids"`
	}
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	var kpi *commonv1.KPI
	var err error
	switch {
	case in.KPIID != "":
		kpi, err = a.hypotheses.GetKPI(r.Context(), ownerID, in.KPIID)
	case strings.TrimSpace(in.KPITitle) != "":
		// Метрика и описание расширяют поисковый запрос: кластерная генерация найдёт больше доказательств,
		// чем по голому названию.
		kpi, err = a.hypotheses.CreateKPI(r.Context(), ownerID, &dbv1.CreateKPIRequest{
			Title: in.KPITitle, Metric: in.KPIMetric, Description: in.KPIDescription,
		})
	default:
		httpx.Error(w, http.StatusBadRequest, "kpi_id or kpi_title is required")
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	created, gerr := a.hypotheses.GenerateFromDocuments(r.Context(), ownerID, kpi, in.Count, in.Constraints, in.DocumentIDs)
	if gerr != nil {
		a.fail(w, gerr)
		return
	}
	a.triggerHypothesisITC(r.Context(), len(created))
	httpx.JSON(w, http.StatusCreated, newHypothesisViews(created))
}

func (a *API) triggerHypothesisITC(ctx context.Context, count int) {
	if a.metrics == nil || count == 0 {
		return
	}
	triggeredAt := time.Now().UTC().Format(time.RFC3339Nano)
	value := fmt.Sprintf("hypotheses-generate:%d:%s", count, triggeredAt)
	_ = a.metrics.EnqueueWork(ctx, itcTriggerKey, value)
}

func (a *API) verifyHypothesis(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.hypothesisActOwner(w, r)
	if !ok {
		return
	}
	h, err := a.hypotheses.Verify(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	// View собирается до постановки фоновых задач: они могут менять h конкурентно (у каталога
	// в процессе указатель общий).
	view := newHypothesisView(h)
	if a.hypJobs != nil {
		a.hypJobs.EnsureAssessTRL(r.Context(), ownerID, []*commonv1.Hypothesis{h}, 1)
		a.hypJobs.EnsureEnrich(r.Context(), ownerID, []*commonv1.Hypothesis{h}, 1)
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (a *API) enrichHypothesis(w http.ResponseWriter, r *http.Request) {
	a.runHypothesisAction(w, r, a.hypotheses.EnrichHypothesis)
}

func (a *API) assessHypothesisTRL(w http.ResponseWriter, r *http.Request) {
	a.runHypothesisAction(w, r, a.hypotheses.AssessTRL)
}

func (a *API) analyzeCompetitors(w http.ResponseWriter, r *http.Request) {
	a.runHypothesisAction(w, r, a.hypotheses.AnalyzeCompetitors)
}

func (a *API) tagHypothesis(w http.ResponseWriter, r *http.Request) {
	a.runHypothesisAction(w, r, a.hypotheses.TagHypothesis)
}

func (a *API) planExperiment(w http.ResponseWriter, r *http.Request) {
	a.runHypothesisAction(w, r, a.hypotheses.PlanExperiment)
}

func (a *API) refineHypothesis(w http.ResponseWriter, r *http.Request) {
	a.runHypothesisAction(w, r, a.hypotheses.Refine)
}

func (a *API) runHypothesisAction(
	w http.ResponseWriter, r *http.Request,
	action func(context.Context, string, string) (*commonv1.Hypothesis, error),
) {
	ownerID, ok := a.hypothesisActOwner(w, r)
	if !ok {
		return
	}
	h, err := action(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newHypothesisView(h))
}

func (a *API) trlRubric(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(a.hypotheses.TRLRubric())
}

func (a *API) storeHypothesisITC(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.hypothesisActOwner(w, r)
	if !ok {
		return
	}
	var in application.ITCInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := a.hypotheses.StoreITC(r.Context(), ownerID, r.PathValue("id"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newHypothesisView(h))
}

func (a *API) itcRubric(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(a.hypotheses.ITCRubric())
}

func (a *API) getHypothesis(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	h, err := a.hypotheses.GetHypothesis(r.Context(), ownerID, a.hypothesesPrivileged(r), r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	if a.hypJobs != nil {
		owner := h.GetOwnerId()
		a.hypJobs.EnsureVerify(r.Context(), owner, []*commonv1.Hypothesis{h}, 1)
		a.hypJobs.EnsureAssessTRL(r.Context(), owner, []*commonv1.Hypothesis{h}, 1)
		a.hypJobs.EnsureEnrich(r.Context(), owner, []*commonv1.Hypothesis{h}, 1)
	}
	httpx.JSON(w, http.StatusOK, newHypothesisView(h))
}

func (a *API) updateHypothesis(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.hypothesisActOwner(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var in hypothesisInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	req := &dbv1.UpdateHypothesisRequest{
		Hypothesis: in.toProto(id),
		Revision:   in.Revision.toProto(id),
	}
	if err := a.hypotheses.UpdateHypothesis(r.Context(), ownerID, req); err != nil {
		a.fail(w, err)
		return
	}
	h, err := a.hypotheses.GetHypothesis(r.Context(), ownerID, false, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newHypothesisView(h))
}

func (a *API) listHypothesisRevisions(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	revs, err := a.hypotheses.ListRevisions(r.Context(), ownerID, a.hypothesesPrivileged(r), r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newRevisionViews(revs))
}

func (a *API) addHypothesisRevision(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.hypothesisActOwner(w, r)
	if !ok {
		return
	}
	var in revisionInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Action == "" {
		httpx.Error(w, http.StatusBadRequest, "action is required")
		return
	}
	rev, err := a.hypotheses.AddRevision(r.Context(), ownerID, in.toProto(r.PathValue("id")))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, newRevisionView(rev))
}

func (a *API) hypothesisGraph(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	g, err := a.hypotheses.Graph(r.Context(), ownerID)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newGraphView(g))
}

func (a *API) listHypothesisEvidence(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	ev, err := a.hypotheses.ListEvidence(r.Context(), ownerID, a.hypothesesPrivileged(r), r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newEvidenceViews(ev))
}
