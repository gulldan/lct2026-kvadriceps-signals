package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/example/main-service/internal/platform/httpx"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

// graphHypotheses отдаёт черновики направлений-мостов из графа знаний владельца; здесь они не сохраняются.
func (a *API) graphHypotheses(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	bridges, err := a.hypotheses.GraphHypotheses(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"candidates": bridges})
}

func (a *API) listKPIs(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	ks, err := a.hypotheses.ListKPIs(r.Context(), ownerID)
	if err == nil && a.hypothesesPrivileged(r) {
		ks, err = a.allKPIs(r.Context())
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newKPIViews(ks))
}

// allKPIs собирает тезисы всех учёток для надзора админа, как у гипотез: db-service фильтрует
// тезисы только по владельцу.
// ponytail: запрос на каждую учётку; на стенде их единицы, общий запрос в db-service — когда станет много.
func (a *API) allKPIs(ctx context.Context) ([]*commonv1.KPI, error) {
	users, err := a.admin.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	var out []*commonv1.KPI
	for _, u := range users {
		ks, err := a.hypotheses.ListKPIs(ctx, u.GetId())
		if err != nil {
			return nil, err
		}
		out = append(out, ks...)
	}
	slices.SortFunc(out, func(x, y *commonv1.KPI) int { return strings.Compare(y.GetCreatedAt(), x.GetCreatedAt()) })
	return out, nil
}

// kpiReadOwner для админа возвращает владельца тезиса: читать чужие тезисы можно, менять — нет.
func (a *API) kpiReadOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return "", false
	}
	if !a.hypothesesPrivileged(r) {
		return ownerID, true
	}
	all, err := a.allKPIs(r.Context())
	if err != nil {
		a.fail(w, err)
		return "", false
	}
	for _, k := range all {
		if k.GetId() == r.PathValue("id") {
			return k.GetOwnerId(), true
		}
	}
	return ownerID, true
}

func (a *API) createKPI(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in kpiInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Title == "" {
		httpx.Error(w, http.StatusBadRequest, "title is required")
		return
	}
	k, err := a.hypotheses.CreateKPI(r.Context(), ownerID, in.toCreateRequest())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, newKPIView(k))
}

func (a *API) getKPI(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.kpiReadOwner(w, r)
	if !ok {
		return
	}
	k, err := a.hypotheses.GetKPI(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newKPIView(k))
}

func (a *API) updateKPI(w http.ResponseWriter, r *http.Request) {
	a.updateOwned(w, r,
		func(ctx context.Context, ownerID, id string, in kpiInput) error {
			return a.hypotheses.UpdateKPI(ctx, ownerID, in.toProto(id))
		},
		a.hypotheses.GetKPI, newKPIView)
}

func (a *API) deleteKPI(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if err := a.hypotheses.DeleteKPI(r.Context(), ownerID, r.PathValue("id")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type attachKPIDocumentsRequest struct {
	DocumentIDs []string `json:"document_ids"`
}

func (a *API) attachKPIDocuments(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in attachKPIDocumentsRequest
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(in.DocumentIDs) == 0 {
		httpx.Error(w, http.StatusBadRequest, "document_ids is required")
		return
	}
	if err := a.hypotheses.AttachKPIDocuments(r.Context(), ownerID, r.PathValue("id"), in.DocumentIDs); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listKPIDocuments(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.kpiReadOwner(w, r)
	if !ok {
		return
	}
	links, err := a.hypotheses.ListKPIDocuments(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]kpiDocumentView, 0, len(links))
	for _, l := range links {
		out = append(out, newKPIDocumentView(l))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"documents": out})
}

func (a *API) detachKPIDocument(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if err := a.hypotheses.DetachKPIDocument(r.Context(), ownerID, r.PathValue("id"), r.PathValue("docId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type kpiView struct {
	ID           string          `json:"id"`
	OwnerID      string          `json:"owner_id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Metric       string          `json:"metric"`
	Unit         string          `json:"unit"`
	Direction    string          `json:"direction"`
	Baseline     *float64        `json:"baseline"`
	Target       *float64        `json:"target"`
	FunctionArea string          `json:"function_area"`
	Status       string          `json:"status"`
	Detail       json.RawMessage `json:"detail"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

func newKPIView(k *commonv1.KPI) kpiView {
	return kpiView{
		ID: k.GetId(), OwnerID: k.GetOwnerId(), Title: k.GetTitle(), Description: k.GetDescription(),
		Metric: k.GetMetric(), Unit: k.GetUnit(), Direction: k.GetDirection(), Baseline: k.Baseline,
		Target: k.Target, FunctionArea: k.GetFunctionArea(), Status: k.GetStatus(),
		Detail: rawJSON(k.GetDetail()), CreatedAt: k.GetCreatedAt(), UpdatedAt: k.GetUpdatedAt(),
	}
}

func newKPIViews(ks []*commonv1.KPI) []kpiView {
	out := make([]kpiView, 0, len(ks))
	for _, k := range ks {
		out = append(out, newKPIView(k))
	}
	return out
}

type kpiInput struct {
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Metric       string          `json:"metric"`
	Unit         string          `json:"unit"`
	Direction    string          `json:"direction"`
	Baseline     *float64        `json:"baseline"`
	Target       *float64        `json:"target"`
	FunctionArea string          `json:"function_area"`
	Status       string          `json:"status"`
	Detail       json.RawMessage `json:"detail"`
}

func (in kpiInput) toCreateRequest() *dbv1.CreateKPIRequest {
	return &dbv1.CreateKPIRequest{
		Title: in.Title, Description: in.Description, Metric: in.Metric, Unit: in.Unit,
		Direction: in.Direction, Baseline: in.Baseline, Target: in.Target,
		FunctionArea: in.FunctionArea, Detail: jsonStr(in.Detail),
	}
}

func (in kpiInput) toProto(id string) *commonv1.KPI {
	return &commonv1.KPI{
		Id: id, Title: in.Title, Description: in.Description, Metric: in.Metric, Unit: in.Unit,
		Direction: in.Direction, Baseline: in.Baseline, Target: in.Target,
		FunctionArea: in.FunctionArea, Status: in.Status, Detail: jsonStr(in.Detail),
	}
}

type kpiDocumentView struct {
	documentView
	Role       string `json:"role"`
	AttachedAt string `json:"attached_at"`
}

func newKPIDocumentView(l *dbv1.KpiDocumentLink) kpiDocumentView {
	return kpiDocumentView{
		documentView: newDocumentView(l.GetDocument()),
		Role:         l.GetRole(),
		AttachedAt:   l.GetAttachedAt(),
	}
}
