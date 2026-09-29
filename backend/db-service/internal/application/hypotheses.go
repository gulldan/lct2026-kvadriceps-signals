package application

import (
	"context"
	"fmt"
	"github.com/example/db-service/internal/platform/contracts"
	"strings"
	"uuid"

	"github.com/example/db-service/internal/domain"
)

// TRL вне диапазона отклоняется, а не обрезается до int16.
const (
	trlMin = 1
	trlMax = 9
)

func validateTRL(trl *int) error {
	if trl == nil {
		return nil
	}
	if *trl < trlMin || *trl > trlMax {
		return fmt.Errorf("trl must be between %d and %d: %w", trlMin, trlMax, domain.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) CreateKPI(ctx context.Context, k *domain.KPI) (*domain.KPI, error) {
	if k.OwnerID == "" || k.Title == "" {
		return nil, fmt.Errorf("owner_id and title are required: %w", domain.ErrInvalidArgument)
	}
	k.ID = uuid.NewV4().String()
	if lease := contracts.HypothesisLeaseFrom(ctx); lease.ID != "" {
		k.ID = contracts.StableID("job-kpi-v1", lease.ID)
	}
	if k.Direction == "" {
		k.Direction = domain.DirectionIncrease
	}
	if k.Status == "" {
		k.Status = domain.StatusActive
	}
	k.CreatedAt = now()
	k.UpdatedAt = now()
	if err := s.kpis.Create(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Service) GetKPI(ctx context.Context, id string) (*domain.KPI, error) {
	return s.kpis.Get(ctx, id)
}

func (s *Service) ListKPIs(ctx context.Context, ownerID string) ([]*domain.KPI, error) {
	return s.kpis.ListByOwner(ctx, ownerID)
}

func (s *Service) UpdateKPI(ctx context.Context, k *domain.KPI) error {
	if k.ID == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	if k.Direction == "" {
		k.Direction = domain.DirectionIncrease
	}
	if k.Status == "" {
		k.Status = domain.StatusActive
	}
	return s.kpis.Update(ctx, k)
}

// DeleteKPI: связанные гипотезы остаются, их kpi_id обнуляет внешний ключ.
func (s *Service) DeleteKPI(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.kpis.Delete(ctx, id)
}

func (s *Service) AttachKPIDocuments(ctx context.Context, readerID, kpiID string, documentIDs []string, role string) error {
	if strings.TrimSpace(readerID) == "" || len(readerID) > 256 || kpiID == "" ||
		len(documentIDs) == 0 || len(documentIDs) > 256 {
		return fmt.Errorf("reader, kpi and 1..256 document IDs are required: %w", domain.ErrInvalidArgument)
	}
	if role == "" {
		role = domain.KPIDocumentRoleInput
	}
	if role != domain.KPIDocumentRoleInput && role != "reference" {
		return fmt.Errorf("invalid document role: %w", domain.ErrInvalidArgument)
	}
	for _, id := range documentIDs {
		if strings.TrimSpace(id) == "" || len(id) > 256 {
			return fmt.Errorf("invalid document ID: %w", domain.ErrInvalidArgument)
		}
	}
	return s.kpis.AttachDocuments(ctx, readerID, kpiID, documentIDs, role)
}

func (s *Service) ListKPIDocuments(ctx context.Context, readerID, kpiID string) ([]*domain.KPIDocumentLink, error) {
	if strings.TrimSpace(readerID) == "" || len(readerID) > 256 || kpiID == "" {
		return nil, fmt.Errorf("reader_id and kpi_id are required: %w", domain.ErrInvalidArgument)
	}
	return s.kpis.ListDocuments(ctx, readerID, kpiID)
}

func (s *Service) DetachKPIDocument(ctx context.Context, kpiID, documentID string) error {
	if kpiID == "" || documentID == "" {
		return fmt.Errorf("kpi_id and document_id are required: %w", domain.ErrInvalidArgument)
	}
	return s.kpis.DetachDocument(ctx, kpiID, documentID)
}

func (s *Service) CreateCluster(ctx context.Context, c *domain.Cluster) (*domain.Cluster, error) {
	if c.OwnerID == "" || c.Label == "" {
		return nil, fmt.Errorf("owner_id and label are required: %w", domain.ErrInvalidArgument)
	}
	c.ID = uuid.NewV4().String()
	if c.Status == "" {
		c.Status = domain.StatusActive
	}
	c.CreatedAt = now()
	c.UpdatedAt = now()
	if err := s.clusters.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) GetCluster(ctx context.Context, id string) (*domain.Cluster, error) {
	return s.clusters.Get(ctx, id)
}

func (s *Service) ListClusters(ctx context.Context, ownerID string) ([]*domain.Cluster, error) {
	return s.clusters.ListByOwner(ctx, ownerID)
}

func (s *Service) UpdateCluster(ctx context.Context, c *domain.Cluster) error {
	if c.ID == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	if c.Status == "" {
		c.Status = domain.StatusActive
	}
	return s.clusters.Update(ctx, c)
}

func (s *Service) DeleteClusters(ctx context.Context, ownerID string) (int64, error) {
	if ownerID == "" {
		return 0, fmt.Errorf("owner_id is required: %w", domain.ErrInvalidArgument)
	}
	return s.clusters.DeleteByOwner(ctx, ownerID)
}

func (s *Service) CreateHypothesis(
	ctx context.Context, h *domain.Hypothesis, initial *domain.Revision,
) (*domain.Hypothesis, error) {
	if h.OwnerID == "" || h.Title == "" || h.Statement == "" {
		return nil, fmt.Errorf("owner_id, title and statement are required: %w", domain.ErrInvalidArgument)
	}
	if err := validateTRL(h.TRL); err != nil {
		return nil, err
	}
	h.ID = uuid.NewV4().String()
	if lease := contracts.HypothesisLeaseFrom(ctx); lease.ID != "" {
		h.ID = contracts.StableID("job-hypothesis-v1", lease.ID, lease.Step)
	}
	if h.Method == "" {
		h.Method = domain.MethodClusterKPI
	}
	if h.Status == "" {
		h.Status = domain.HypothesisGenerated
	}
	h.CreatedAt = now()
	h.UpdatedAt = now()
	for _, e := range h.Evidence {
		e.ID = uuid.NewV4().String()
		e.HypothesisID = h.ID
		if e.Stance == "" {
			e.Stance = domain.StanceSupports
		}
		e.CreatedAt = now()
	}
	if initial != nil {
		initial.ID = uuid.NewV4().String()
		initial.HypothesisID = h.ID
		if initial.Action == "" {
			initial.Action = domain.ActionCreated
		}
		initial.CreatedAt = now()
	}
	if err := s.hypotheses.Create(ctx, h, initial); err != nil {
		return nil, err
	}
	return h, nil
}

func (s *Service) GetHypothesis(ctx context.Context, id string) (*domain.Hypothesis, error) {
	return s.hypotheses.Get(ctx, id)
}

// ListHypotheses: пустой ownerID — доска всех владельцев.
func (s *Service) ListHypotheses(
	ctx context.Context, f domain.HypothesisFilter,
) ([]*domain.Hypothesis, error) {
	if f.BeforeID != "" && f.OrderBy != "created_at" {
		return nil, domain.ErrInvalidArgument
	}

	return s.hypotheses.List(ctx, f)
}

func (s *Service) UpdateHypothesis(ctx context.Context, h *domain.Hypothesis, rev *domain.Revision) error {
	if h.ID == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	if err := validateTRL(h.TRL); err != nil {
		return err
	}
	if h.Method == "" {
		h.Method = domain.MethodClusterKPI
	}
	if h.Status == "" {
		h.Status = domain.HypothesisGenerated
	}
	h.UpdatedAt = now()
	if rev != nil {
		rev.ID = uuid.NewV4().String()
		rev.HypothesisID = h.ID
		if rev.Action == "" {
			rev.Action = domain.ActionEdited
		}
		rev.CreatedAt = now()
	}
	return s.hypotheses.Update(ctx, h, rev)
}

func (s *Service) AddHypothesisRevision(ctx context.Context, rev *domain.Revision) (*domain.Revision, error) {
	if rev.HypothesisID == "" || rev.Action == "" {
		return nil, fmt.Errorf("hypothesis_id and action are required: %w", domain.ErrInvalidArgument)
	}
	rev.ID = uuid.NewV4().String()
	rev.CreatedAt = now()
	if err := s.hypotheses.AddRevision(ctx, rev); err != nil {
		return nil, err
	}
	return rev, nil
}

func (s *Service) ListHypothesisRevisions(ctx context.Context, hypothesisID string) ([]*domain.Revision, error) {
	return s.hypotheses.ListRevisions(ctx, hypothesisID)
}

func (s *Service) ListHypothesisEvidence(ctx context.Context, hypothesisID string) ([]*domain.Evidence, error) {
	return s.hypotheses.ListEvidence(ctx, hypothesisID)
}

func (s *Service) HypothesisBoard(ctx context.Context, f domain.HypothesisFilter) ([]*domain.Hypothesis, []byte, error) {
	return s.hypotheses.Board(ctx, f)
}
