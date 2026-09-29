package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	"github.com/example/main-service/internal/platform/httpx"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func (a *API) listClusters(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.boardOwner(w, r)
	if !ok {
		return
	}
	cs, err := a.hypotheses.ListClusters(r.Context(), ownerID)
	if err != nil {
		a.fail(w, err)
		return
	}
	// ?limit= — витрине нужна верхушка, а полный список весит больше мегабайта. Сортировка устойчивая,
	// иначе порядок тем с равным числом документов плыл бы вместе с валидатором кэша.
	if n, _ := strconv.Atoi(r.URL.Query().Get("limit")); n > 0 {
		slices.SortStableFunc(cs, func(x, y *commonv1.Cluster) int {
			return cmp.Compare(y.GetDocumentCount(), x.GetDocumentCount())
		})
		cs = cs[:min(n, len(cs))]
	}
	// Список тем меняется только после пересчёта кластеров, до него повтор обходится валидатором.
	if r.URL.Query().Get("view") == "full" {
		a.cachedJSON(w, r, newClusterViews(cs))
		return
	}
	a.cachedJSON(w, r, newClusterListViews(cs))
}

// boardOwner выбирает, чью доску тем читать. Админу без своей доски отдаём опубликованную доску
// стенда: её строит cluster-worker от имени своей учётки, и иначе админ видел бы «Тем пока нет».
// Запись и публикация остаются за владельцем, поэтому clusterHead владельца не подменяет.
func (a *API) boardOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return "", false
	}
	if !a.hypothesesPrivileged(r) {
		return ownerID, true
	}
	if head, err := a.hypotheses.ClusterHead(r.Context(), ownerID); err != nil || head != "" {
		return ownerID, true
	}
	users, err := a.admin.ListUsers(r.Context())
	if err != nil {
		a.fail(w, err)
		return "", false
	}
	for _, u := range users {
		if head, err := a.hypotheses.ClusterHead(r.Context(), u.GetId()); err == nil && head != "" {
			return u.GetId(), true
		}
	}
	return ownerID, true
}

func (a *API) clusterHead(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	id, err := a.hypotheses.ClusterHead(r.Context(), ownerID)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"publication_id": id})
}

func (a *API) createCluster(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in clusterInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Label == "" {
		httpx.Error(w, http.StatusBadRequest, "label is required")
		return
	}
	c, err := a.hypotheses.CreateCluster(r.Context(), ownerID, in.toCreateRequest())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, newClusterView(c))
}

func (a *API) replaceClusters(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in struct {
		RequestID             string         `json:"request_id"`
		ExpectedPublicationID string         `json:"expected_publication_id"`
		Clusters              []clusterInput `json:"clusters"`
	}
	// Доска публикуется целиком (членство и представители сотен кластеров), 1 МиБ ей мало.
	if err := decodeResourceLimited(r, &in, 64<<20); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	req := &dbv1.PublishClustersRequest{RequestId: in.RequestID, ExpectedPublicationId: in.ExpectedPublicationID}
	for i := range in.Clusters {
		req.Clusters = append(req.Clusters, in.Clusters[i].toCreateRequest())
	}
	created, err := a.hypotheses.PublishClusters(r.Context(), ownerID, req)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, newClusterViews(created))
}

func (a *API) getCluster(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := a.boardOwner(w, r)
	if !ok {
		return
	}
	c, err := a.hypotheses.GetCluster(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newClusterView(c))
}

func (a *API) updateCluster(w http.ResponseWriter, r *http.Request) {
	a.updateOwned(w, r,
		func(ctx context.Context, ownerID, id string, in clusterInput) error {
			return a.hypotheses.UpdateCluster(ctx, ownerID, in.toProto(id))
		},
		a.hypotheses.GetCluster, newClusterView)
}

// deleteClusters — ручная чистка оператором; перекластеризация публикует версии через replaceClusters.
func (a *API) deleteClusters(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if err := a.hypotheses.DeleteAllClusters(r.Context(), ownerID); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type clusterView struct {
	ID              string          `json:"id"`
	OwnerID         string          `json:"owner_id"`
	Label           string          `json:"label"`
	Summary         string          `json:"summary"`
	Keywords        []string        `json:"keywords"`
	Method          string          `json:"method"`
	ChunkCount      int32           `json:"chunk_count"`
	DocumentCount   int32           `json:"document_count"`
	Representatives json.RawMessage `json:"representatives"`
	Params          json.RawMessage `json:"params"`
	Status          string          `json:"status"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

func newClusterView(c *commonv1.Cluster) clusterView {
	return clusterView{
		ID: c.GetId(), OwnerID: c.GetOwnerId(), Label: c.GetLabel(), Summary: c.GetSummary(),
		Keywords: orEmptyStrings(c.GetKeywords()), Method: c.GetMethod(), ChunkCount: c.GetChunkCount(),
		DocumentCount: c.GetDocumentCount(), Representatives: rawJSON(c.GetRepresentatives()),
		Params: rawJSON(c.GetParams()), Status: c.GetStatus(),
		CreatedAt: c.GetCreatedAt(), UpdatedAt: c.GetUpdatedAt(),
	}
}

func newClusterViews(cs []*commonv1.Cluster) []clusterView {
	out := make([]clusterView, 0, len(cs))
	for _, c := range cs {
		out = append(out, newClusterView(c))
	}
	return out
}

// newClusterListViews: описание, сниппеты и полный params отдаёт только GET /clusters/{id} — списки их
// не рисуют, а весят они больше половины ответа. Пустое summary не ломает разбор ответа клиентом;
// fingerprint нужен discovery-worker для сведения мостов графа с темами, а признаки доступности
// сигналов отличают на карте измерение от отсутствия данных.
func newClusterListViews(cs []*commonv1.Cluster) []clusterView {
	out := make([]clusterView, 0, len(cs))
	for _, c := range cs {
		v := newClusterView(c)
		v.Summary = ""
		v.Representatives = json.RawMessage("[]")
		v.Params = projectJSONBranches(c.GetParams(),
			[]string{"fingerprint"},
			[]string{keyITC, "techscore"}, []string{keyITC, "axes"},
			[]string{keyITC, keySignals, "pub_count"}, []string{keyITC, keySignals, "org_count"},
			[]string{keyITC, keySignals, "freshness_available"},
			[]string{keyITC, keySignals, "novelty_available"})
		out = append(out, v)
	}
	return out
}

type clusterInput struct {
	Label           string          `json:"label"`
	Summary         string          `json:"summary"`
	Keywords        []string        `json:"keywords"`
	Method          string          `json:"method"`
	ChunkCount      int32           `json:"chunk_count"`
	DocumentCount   int32           `json:"document_count"`
	Representatives json.RawMessage `json:"representatives"`
	Params          json.RawMessage `json:"params"`
	Status          string          `json:"status"`
}

func (in clusterInput) toCreateRequest() *dbv1.CreateClusterRequest {
	return &dbv1.CreateClusterRequest{
		Label: in.Label, Summary: in.Summary, Keywords: in.Keywords, Method: in.Method,
		ChunkCount: in.ChunkCount, DocumentCount: in.DocumentCount,
		Representatives: jsonStr(in.Representatives), Params: jsonStr(in.Params),
	}
}

func (in clusterInput) toProto(id string) *commonv1.Cluster {
	return &commonv1.Cluster{
		Id: id, Label: in.Label, Summary: in.Summary, Keywords: in.Keywords, Method: in.Method,
		ChunkCount: in.ChunkCount, DocumentCount: in.DocumentCount,
		Representatives: jsonStr(in.Representatives), Params: jsonStr(in.Params), Status: in.Status,
	}
}
