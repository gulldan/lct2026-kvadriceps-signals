package application

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	"sync"

	"github.com/example/main-service/internal/platform/dbclient"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

// fakeCatalog не фильтрует по владельцу: это задача слоя приложения, а не хранилища.
type fakeCatalog struct {
	mu          sync.Mutex
	kpis        map[string]*commonv1.KPI
	clusters    map[string]*commonv1.Cluster
	hypotheses  map[string]*commonv1.Hypothesis
	revisions   map[string][]*commonv1.HypothesisRevision
	evidence    map[string][]*commonv1.HypothesisEvidence
	list        []*commonv1.Hypothesis
	updateCalls int
	kpiDocs     map[string][]*dbv1.KpiDocumentLink
	docs        []*commonv1.Document

	createKPIErr        error
	getKPIErr           error
	listKPIErr          error
	updateKPIErr        error
	createClusterErr    error
	getClusterErr       error
	listClusterErr      error
	updateClusterErr    error
	deleteClustersErr   error
	createHypErr        error
	getHypErr           error
	listHypErr          error
	updateHypErr        error
	addRevErr           error
	listRevErr          error
	listEvidenceErr     error
	deleteClustersCount int
}

func newCatalog() *fakeCatalog {
	return &fakeCatalog{
		kpis:       map[string]*commonv1.KPI{},
		clusters:   map[string]*commonv1.Cluster{},
		hypotheses: map[string]*commonv1.Hypothesis{},
		revisions:  map[string][]*commonv1.HypothesisRevision{},
		evidence:   map[string][]*commonv1.HypothesisEvidence{},
	}
}

func (c *fakeCatalog) CreateKPI(_ context.Context, req *dbv1.CreateKPIRequest) (*commonv1.KPI, error) {
	if c.createKPIErr != nil {
		return nil, c.createKPIErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := &commonv1.KPI{
		Id: "kpi-" + req.GetTitle(), OwnerId: req.GetOwnerId(), Title: req.GetTitle(),
		Metric: req.GetMetric(), Description: req.GetDescription(), FunctionArea: req.GetFunctionArea(),
	}
	c.kpis[k.GetId()] = k
	return k, nil
}

func (c *fakeCatalog) GetKPI(_ context.Context, id string) (*commonv1.KPI, error) {
	if c.getKPIErr != nil {
		return nil, c.getKPIErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.kpis[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return k, nil
}

func (c *fakeCatalog) ListKPIs(_ context.Context, ownerID string) ([]*commonv1.KPI, error) {
	if c.listKPIErr != nil {
		return nil, c.listKPIErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*commonv1.KPI, 0, len(c.kpis))
	for _, k := range c.kpis {
		if k.GetOwnerId() == ownerID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (c *fakeCatalog) UpdateKPI(_ context.Context, kpi *commonv1.KPI) error {
	if c.updateKPIErr != nil {
		return c.updateKPIErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kpis[kpi.GetId()] = kpi
	return nil
}

func (c *fakeCatalog) DeleteKPI(_ context.Context, id string) error {
	delete(c.kpis, id)
	return nil
}

func (c *fakeCatalog) AttachKPIDocuments(_ context.Context, readerID, kpiID string, documentIDs []string, role string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.kpiDocs == nil {
		c.kpiDocs = map[string][]*dbv1.KpiDocumentLink{}
	}
	if k := c.kpis[kpiID]; k == nil || k.GetOwnerId() != readerID {
		return dbclient.ErrNotFound
	}
	for _, documentID := range documentIDs {
		c.kpiDocs[kpiID] = append(c.kpiDocs[kpiID], &dbv1.KpiDocumentLink{
			Document: &commonv1.Document{Id: documentID}, Role: role,
		})
	}
	return nil
}

func (c *fakeCatalog) ListKPIDocuments(_ context.Context, _, kpiID string) ([]*dbv1.KpiDocumentLink, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.kpiDocs[kpiID], nil
}

func (c *fakeCatalog) ListDocuments(_ context.Context, _ string, _ ...contracts.DocumentScan) ([]*commonv1.Document, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.docs, nil
}

func (c *fakeCatalog) DetachKPIDocument(_ context.Context, kpiID, documentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.kpiDocs[kpiID][:0]
	for _, l := range c.kpiDocs[kpiID] {
		if l.GetDocument().GetId() != documentID {
			kept = append(kept, l)
		}
	}
	c.kpiDocs[kpiID] = kept
	return nil
}

func (c *fakeCatalog) CreateCluster(_ context.Context, req *dbv1.CreateClusterRequest) (*commonv1.Cluster, error) {
	if c.createClusterErr != nil {
		return nil, c.createClusterErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cl := &commonv1.Cluster{
		Id: "cl-" + req.GetLabel(), OwnerId: req.GetOwnerId(), Label: req.GetLabel(),
		Summary: req.GetSummary(), Params: req.GetParams(),
	}
	c.clusters[cl.GetId()] = cl
	return cl, nil
}

func (c *fakeCatalog) GetCluster(_ context.Context, id string) (*commonv1.Cluster, error) {
	if c.getClusterErr != nil {
		return nil, c.getClusterErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cl, ok := c.clusters[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return cl, nil
}

func (c *fakeCatalog) ListClusters(_ context.Context, ownerID string) ([]*commonv1.Cluster, error) {
	if c.listClusterErr != nil {
		return nil, c.listClusterErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*commonv1.Cluster, 0, len(c.clusters))
	for _, cl := range c.clusters {
		if cl.GetOwnerId() == ownerID {
			out = append(out, cl)
		}
	}
	return out, nil
}

func (c *fakeCatalog) UpdateCluster(_ context.Context, cluster *commonv1.Cluster) error {
	if c.updateClusterErr != nil {
		return c.updateClusterErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clusters[cluster.GetId()] = cluster
	return nil
}

func (c *fakeCatalog) DeleteClusters(_ context.Context, ownerID string) error {
	c.mu.Lock()
	c.deleteClustersCount++
	c.mu.Unlock()
	if c.deleteClustersErr != nil {
		return c.deleteClustersErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, cl := range c.clusters {
		if cl.GetOwnerId() == ownerID {
			delete(c.clusters, id)
		}
	}
	return nil
}

func (c *fakeCatalog) CreateHypothesis(
	_ context.Context, req *dbv1.CreateHypothesisRequest,
) (*commonv1.Hypothesis, error) {
	if c.createHypErr != nil {
		return nil, c.createHypErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	h := req.GetHypothesis()
	if h.GetId() == "" {
		h.Id = "hyp-" + h.GetTitle()
	}
	c.hypotheses[h.GetId()] = h
	if len(h.GetEvidence()) > 0 {
		c.evidence[h.GetId()] = h.GetEvidence()
	}
	return h, nil
}

func (c *fakeCatalog) GetHypothesis(_ context.Context, id string) (*commonv1.Hypothesis, error) {
	if c.getHypErr != nil {
		return nil, c.getHypErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.hypotheses[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return h, nil
}

func (c *fakeCatalog) ListHypotheses(
	_ context.Context, req *dbv1.ListHypothesesRequest,
) ([]*commonv1.Hypothesis, error) {
	if c.listHypErr != nil {
		return nil, c.listHypErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.list != nil {
		return c.list, nil
	}
	out := make([]*commonv1.Hypothesis, 0, len(c.hypotheses))
	for _, h := range c.hypotheses {
		if req.GetOwnerId() == "" || h.GetOwnerId() == req.GetOwnerId() {
			out = append(out, h)
		}
	}
	return out, nil
}

func (c *fakeCatalog) UpdateHypothesis(_ context.Context, req *dbv1.UpdateHypothesisRequest) error {
	c.mu.Lock()
	c.updateCalls++
	c.mu.Unlock()
	if c.updateHypErr != nil {
		return c.updateHypErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	h := req.GetHypothesis()
	c.hypotheses[h.GetId()] = h
	return nil
}

func (c *fakeCatalog) AddHypothesisRevision(
	_ context.Context, rev *commonv1.HypothesisRevision,
) (*commonv1.HypothesisRevision, error) {
	if c.addRevErr != nil {
		return nil, c.addRevErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rev.Id = "rev"
	c.revisions[rev.GetHypothesisId()] = append(c.revisions[rev.GetHypothesisId()], rev)
	return rev, nil
}

func (c *fakeCatalog) ListHypothesisRevisions(
	_ context.Context, hypothesisID string,
) ([]*commonv1.HypothesisRevision, error) {
	if c.listRevErr != nil {
		return nil, c.listRevErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revisions[hypothesisID], nil
}

func (c *fakeCatalog) ListHypothesisEvidence(
	_ context.Context, hypothesisID string,
) ([]*commonv1.HypothesisEvidence, error) {
	if c.listEvidenceErr != nil {
		return nil, c.listEvidenceErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evidence[hypothesisID], nil
}

// putHypothesis кладёт гипотезу в обход ранжирования CreateHypothesis.
func (c *fakeCatalog) putHypothesis(h *commonv1.Hypothesis) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hypotheses[h.GetId()] = h
}

func (c *fakeCatalog) HypothesisBoard(
	ctx context.Context,
	req *dbv1.ListHypothesesRequest,
) (*dbv1.ListHypothesesResponse, error) {
	hs, err := c.ListHypotheses(ctx, req)
	return &dbv1.ListHypothesesResponse{Hypotheses: hs, Aggregates: []byte(`{}`)}, err
}
