package httpapi_test

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	"sync"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/dbclient"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

type fakeWeights struct {
	mu sync.Mutex
	w  map[string]*application.ScoringWeights
}

func newWeights() *fakeWeights { return &fakeWeights{w: map[string]*application.ScoringWeights{}} }

func (f *fakeWeights) Get(_ context.Context, ownerID string) (*application.ScoringWeights, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.w[ownerID], nil
}

func (f *fakeWeights) Set(_ context.Context, ownerID string, w application.ScoringWeights) error {
	f.mu.Lock()
	f.w[ownerID] = &w
	f.mu.Unlock()
	return nil
}

type fakeRuntimeSettings struct {
	mu       sync.Mutex
	settings map[string]*application.HypothesisRuntimeSettings
}

func newRuntimeSettings() *fakeRuntimeSettings {
	return &fakeRuntimeSettings{settings: map[string]*application.HypothesisRuntimeSettings{}}
}

func (f *fakeRuntimeSettings) Get(_ context.Context, ownerID string) (*application.HypothesisRuntimeSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings[ownerID], nil
}

func (f *fakeRuntimeSettings) Set(_ context.Context, ownerID string, settings application.HypothesisRuntimeSettings) error {
	f.mu.Lock()
	f.settings[ownerID] = &settings
	f.mu.Unlock()
	return nil
}

type fakeGraph struct {
	edges map[string][]application.KGEdge
}

func newGraph() *fakeGraph { return &fakeGraph{edges: map[string][]application.KGEdge{}} }

func (f *fakeGraph) AddEdges(_ context.Context, ownerID string, edges []application.KGEdge) error {
	f.edges[ownerID] = append(f.edges[ownerID], edges...)
	return nil
}

func (f *fakeGraph) Edges(_ context.Context, ownerID string) ([]application.KGEdge, error) {
	return f.edges[ownerID], nil
}

type fakeJobStore struct {
	mu        sync.Mutex
	jobs      map[string]*contracts.HypothesisJob
	order     []string
	claimGate chan struct{}
}

func newJobStore() *fakeJobStore { return &fakeJobStore{jobs: map[string]*contracts.HypothesisJob{}} }
func cloneJob(j *contracts.HypothesisJob) *contracts.HypothesisJob {
	c := *j
	c.ResultIDs = append([]string{}, j.ResultIDs...)
	c.Input.DocumentIDs = append([]string{}, j.Input.DocumentIDs...)
	return &c
}
func (f *fakeJobStore) EnqueueHypothesisJob(_ context.Context, j *contracts.HypothesisJob) (*contracts.HypothesisJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing := f.jobs[j.ID]; existing != nil {
		return cloneJob(existing), nil
	}
	c := cloneJob(j)
	c.Status = "queued"
	c.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	f.jobs[c.ID] = c
	f.order = append([]string{c.ID}, f.order...)
	return cloneJob(c), nil
}
func (f *fakeJobStore) GetHypothesisJob(_ context.Context, owner, id string) (*contracts.HypothesisJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	if j == nil || (owner != "" && owner != j.OwnerID) {
		return nil, dbclient.ErrNotFound
	}
	return cloneJob(j), nil
}
func (f *fakeJobStore) ListHypothesisJobs(_ context.Context, owner string, limit int) ([]*contracts.HypothesisJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*contracts.HypothesisJob{}
	for _, id := range f.order {
		j := f.jobs[id]
		if owner == "" || j.OwnerID == owner {
			out = append(out, cloneJob(j))
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (f *fakeJobStore) ClaimHypothesisJob(ctx context.Context) (*contracts.HypothesisJob, error) {
	f.mu.Lock()
	gate := f.claimGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.order {
		j := f.jobs[id]
		if j.Status == "queued" {
			j.Status = "running"
			j.Token = "lease"
			j.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return cloneJob(j), nil
		}
	}
	return nil, dbclient.ErrNotFound
}
func (f *fakeJobStore) RenewHypothesisJob(context.Context, string, string) error { return nil }
func (f *fakeJobStore) FinishHypothesisJob(_ context.Context, j *contracts.HypothesisJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[j.ID] = cloneJob(j)
	return nil
}

type fakeCatalog struct {
	mu         sync.Mutex
	kpis       map[string]*commonv1.KPI
	clusters   map[string]*commonv1.Cluster
	hypotheses map[string]*commonv1.Hypothesis
	revisions  map[string][]*commonv1.HypothesisRevision
	evidence   map[string][]*commonv1.HypothesisEvidence
	listErr    error
	listKPIErr error
	listClErr  error
	kpiDocs    map[string][]*dbv1.KpiDocumentLink
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
	c.mu.Lock()
	defer c.mu.Unlock()
	k := &commonv1.KPI{Id: "kpi-" + req.GetTitle(), OwnerId: req.GetOwnerId(), Title: req.GetTitle(), Metric: req.GetMetric()}
	c.kpis[k.GetId()] = k
	return k, nil
}

func (c *fakeCatalog) GetKPI(_ context.Context, id string) (*commonv1.KPI, error) {
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
	c.mu.Lock()
	c.kpis[kpi.GetId()] = kpi
	c.mu.Unlock()
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
	return nil, dbclient.ErrNotFound
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
	c.mu.Lock()
	defer c.mu.Unlock()
	id := "cl-" + req.GetLabel()
	if _, dup := c.clusters[id]; dup {
		id += "-" + req.GetParams()
	}
	cl := &commonv1.Cluster{Id: id, OwnerId: req.GetOwnerId(), Label: req.GetLabel(), Params: req.GetParams()}
	c.clusters[cl.GetId()] = cl
	return cl, nil
}

func (c *fakeCatalog) GetCluster(_ context.Context, id string) (*commonv1.Cluster, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl, ok := c.clusters[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return cl, nil
}

func (c *fakeCatalog) ListClusters(_ context.Context, ownerID string) ([]*commonv1.Cluster, error) {
	if c.listClErr != nil {
		return nil, c.listClErr
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
	c.mu.Lock()
	c.clusters[cluster.GetId()] = cluster
	c.mu.Unlock()
	return nil
}

func (c *fakeCatalog) DeleteClusters(_ context.Context, ownerID string) error {
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
	if c.listErr != nil {
		return nil, c.listErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
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
	c.hypotheses[req.GetHypothesis().GetId()] = req.GetHypothesis()
	c.mu.Unlock()
	return nil
}

func (c *fakeCatalog) AddHypothesisRevision(
	_ context.Context, rev *commonv1.HypothesisRevision,
) (*commonv1.HypothesisRevision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rev.Id = "rev"
	c.revisions[rev.GetHypothesisId()] = append(c.revisions[rev.GetHypothesisId()], rev)
	return rev, nil
}

func (c *fakeCatalog) ListHypothesisRevisions(
	_ context.Context, hypothesisID string,
) ([]*commonv1.HypothesisRevision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revisions[hypothesisID], nil
}

func (c *fakeCatalog) ListHypothesisEvidence(
	_ context.Context, hypothesisID string,
) ([]*commonv1.HypothesisEvidence, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evidence[hypothesisID], nil
}

func (c *fakeCatalog) putHypothesis(h *commonv1.Hypothesis) {
	c.mu.Lock()
	c.hypotheses[h.GetId()] = h
	c.mu.Unlock()
}
