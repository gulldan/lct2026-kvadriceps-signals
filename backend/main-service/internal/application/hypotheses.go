// Сервис гипотез проверяет владельца на границе: db-service хранит owner_id, но вызывающего не проверяет.

package application

import (
	"context"
	"strings"
	"uuid"

	"github.com/example/main-service/internal/domain"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"github.com/example/main-service/internal/platform/logger"
	"github.com/example/main-service/internal/platform/runtimecfg"
)

type HypothesisService struct {
	cat       domain.HypothesisCatalog
	answerer  domain.Answerer
	weights   ScoringWeightsStore   // nil — профиль оценки по умолчанию
	settings  RuntimeSettingsStore  // nil — настройки по умолчанию
	materials MaterialsRef          // nil — новизна без сверки с Materials Project
	graph     GraphStore            // nil — пополнение графа и мосты ничего не делают
	chunks    domain.ChunkReader    // nil — обогащение только по контексту поиска
	pubs      PubSearcher           // nil — генерация без открытой литературы
	pubIngest PubIngestor           // nil — найденные публикации не дозаливаются в корпус
	ovr       *runtimecfg.Overrides // nil — флаги генерации из env и умолчаний
}

func (s *HypothesisService) SetRuntimeOverrides(ovr *runtimecfg.Overrides) { s.ovr = ovr }

func NewHypothesisService(
	cat domain.HypothesisCatalog, answerer domain.Answerer,
	weights ScoringWeightsStore, settings RuntimeSettingsStore, materials MaterialsRef, graph GraphStore,
	chunks domain.ChunkReader,
) *HypothesisService {
	return &HypothesisService{
		cat: cat, answerer: answerer, weights: weights, settings: settings,
		materials: materials, graph: graph, chunks: chunks,
	}
}

func (s *HypothesisService) rankingWeights(ctx context.Context, ownerID string) ScoringWeights {
	if s.weights == nil {
		return DefaultWeights()
	}
	w, err := s.weights.Get(ctx, ownerID)
	if err != nil || w == nil {
		return DefaultWeights()
	}
	return *w
}

func (s *HypothesisService) GetScoringWeights(ctx context.Context, ownerID string) ScoringWeights {
	return s.rankingWeights(ctx, ownerID)
}

func (s *HypothesisService) SetScoringWeights(ctx context.Context, ownerID string, w ScoringWeights) error {
	if s.weights == nil {
		return nil
	}
	return s.weights.Set(ctx, ownerID, w)
}

func (s *HypothesisService) runtimeSettings(ctx context.Context, ownerID string) HypothesisRuntimeSettings {
	if s.settings == nil {
		return DefaultHypothesisRuntimeSettings()
	}
	settings, err := s.settings.Get(ctx, ownerID)
	if err != nil || settings == nil {
		return DefaultHypothesisRuntimeSettings()
	}
	return NormalizeHypothesisRuntimeSettings(*settings)
}

func (s *HypothesisService) GetRuntimeSettings(ctx context.Context, ownerID string) HypothesisRuntimeSettings {
	return s.runtimeSettings(ctx, ownerID)
}

func (s *HypothesisService) SetRuntimeSettings(ctx context.Context, ownerID string, settings HypothesisRuntimeSettings) error {
	if s.settings == nil {
		return nil
	}
	return s.settings.Set(ctx, ownerID, NormalizeHypothesisRuntimeSettings(settings))
}

func (s *HypothesisService) applyRanking(ctx context.Context, h *commonv1.Hypothesis) {
	applyRankingWith(h, s.rankingWeights(ctx, h.GetOwnerId()))
}

func (s *HypothesisService) CreateKPI(
	ctx context.Context, ownerID string, req *dbv1.CreateKPIRequest,
) (*commonv1.KPI, error) {
	req.OwnerId = ownerID
	return s.cat.CreateKPI(ctx, req)
}

func (s *HypothesisService) ListKPIs(ctx context.Context, ownerID string) ([]*commonv1.KPI, error) {
	return s.cat.ListKPIs(ctx, ownerID)
}

func (s *HypothesisService) GetKPI(ctx context.Context, ownerID, id string) (*commonv1.KPI, error) {
	k, err := s.cat.GetKPI(ctx, id)
	if err != nil {
		return nil, err
	}
	if k.GetOwnerId() != ownerID {
		return nil, ErrForbidden
	}
	return k, nil
}

// UpdateKPI переносит архивацию и восстановление цели на её гипотезы, чтобы доска не показывала гипотезы неактивной цели.
func (s *HypothesisService) UpdateKPI(ctx context.Context, ownerID string, kpi *commonv1.KPI) error {
	prev, err := s.GetKPI(ctx, ownerID, kpi.GetId())
	if err != nil {
		return err
	}
	kpi.OwnerId = ownerID
	if uerr := s.cat.UpdateKPI(ctx, kpi); uerr != nil {
		return uerr
	}
	if st := kpi.GetStatus(); st != "" && st != prev.GetStatus() {
		s.cascadeKPIStatus(ctx, ownerID, kpi.GetId(), st)
	}
	return nil
}

// DeleteKPI сначала архивирует гипотезы цели: иначе после обнуления kpi_id внешним ключом они останутся сиротами.
func (s *HypothesisService) DeleteKPI(ctx context.Context, ownerID, id string) error {
	if _, err := s.GetKPI(ctx, ownerID, id); err != nil {
		return err
	}
	s.cascadeKPIStatus(ctx, ownerID, id, statusArchived)
	return s.cat.DeleteKPI(ctx, id)
}

const kpiStatusActive = "active"

// cascadeKPIStatus — по возможности: сбой по отдельной гипотезе лишь логируется, изменение цели всё равно проходит.
func (s *HypothesisService) cascadeKPIStatus(ctx context.Context, ownerID, kpiID, kpiStatus string) {
	var target string
	switch kpiStatus {
	case statusArchived:
		target = statusArchived
	case kpiStatusActive:
		target = statusGenerated
	default:
		return
	}
	err := s.walkHypotheses(ctx, &dbv1.ListHypothesesRequest{
		OwnerId: ownerID,
		KpiId:   kpiID,
	}, func(
		refs []*commonv1.Hypothesis,
	) (bool, error) {
		for _, ref := range refs {
			s.cascadeHypothesisStatus(ctx, ref, target)
		}
		return true, nil
	})
	if err != nil {
		logger.From(ctx).Warn().Err(err).Str("kpi_id", kpiID).Msg("kpi cascade: list hypotheses failed")
	}
}

func (s *HypothesisService) cascadeHypothesisStatus(ctx context.Context, ref *commonv1.Hypothesis, target string) {
	if !cascadeFlips(ref.GetStatus(), target) {
		return
	}
	h, gerr := s.cat.GetHypothesis(ctx, ref.GetId())
	if gerr != nil {
		logger.From(ctx).Warn().Err(gerr).Str("hypothesis_id", ref.GetId()).Msg("kpi cascade: get failed")
		return
	}
	h.Status = target
	rev := &commonv1.HypothesisRevision{
		HypothesisId: h.GetId(), Action: "status_changed", EditorId: editorSystem,
		Summary: cascadeSummary(target),
	}
	if uerr := s.cat.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		logger.From(ctx).Warn().Err(uerr).Str("hypothesis_id", h.GetId()).Msg("kpi cascade: update failed")
	}
}

func cascadeFlips(current, target string) bool {
	switch target {
	case statusArchived:
		return current == "draft" || current == statusGenerated || current == "under_review"
	case statusGenerated:
		return current == statusArchived
	}
	return false
}

func cascadeSummary(target string) string {
	if target == statusArchived {
		return "Архивация вместе с целью"
	}
	return "Восстановление вместе с целью"
}

func (s *HypothesisService) AttachKPIDocuments(ctx context.Context, ownerID, kpiID string, documentIDs []string) error {
	if _, err := s.GetKPI(ctx, ownerID, kpiID); err != nil {
		return err
	}
	return s.cat.AttachKPIDocuments(ctx, ownerID, kpiID, documentIDs, "input")
}

func (s *HypothesisService) ListKPIDocuments(ctx context.Context, ownerID, kpiID string) ([]*dbv1.KpiDocumentLink, error) {
	if _, err := s.GetKPI(ctx, ownerID, kpiID); err != nil {
		return nil, err
	}
	return s.cat.ListKPIDocuments(ctx, ownerID, kpiID)
}

func (s *HypothesisService) DetachKPIDocument(ctx context.Context, ownerID, kpiID, documentID string) error {
	if _, err := s.GetKPI(ctx, ownerID, kpiID); err != nil {
		return err
	}
	return s.cat.DetachKPIDocument(ctx, kpiID, documentID)
}

func (s *HypothesisService) CreateCluster(
	ctx context.Context, ownerID string, req *dbv1.CreateClusterRequest,
) (*commonv1.Cluster, error) {
	req.OwnerId = ownerID
	return s.cat.CreateCluster(ctx, req)
}

func (s *HypothesisService) ClusterHead(ctx context.Context, ownerID string) (string, error) {
	return s.cat.ClusterHead(ctx, ownerID)
}

func (s *HypothesisService) PublishClusters(
	ctx context.Context,
	ownerID string,
	req *dbv1.PublishClustersRequest,
) ([]*commonv1.Cluster, error) {
	// Лимит совпадает с публикацией в базе: текущая доска корпуса уже превышает 2000 тем.
	if _, err := uuid.Parse(req.GetRequestId()); err != nil || len(req.GetClusters()) > 5000 {
		return nil, ErrInvalidArgument
	}
	if req.GetExpectedPublicationId() != "" {
		if _, err := uuid.Parse(req.GetExpectedPublicationId()); err != nil {
			return nil, ErrInvalidArgument
		}
	}
	for _, c := range req.GetClusters() {
		if strings.TrimSpace(c.GetLabel()) == "" {
			return nil, ErrInvalidArgument
		}
	}
	req.OwnerId = ownerID
	return s.cat.PublishClusters(ctx, req)
}

func (s *HypothesisService) ListClusters(ctx context.Context, ownerID string) ([]*commonv1.Cluster, error) {
	return s.cat.ListClusters(ctx, ownerID)
}

func (s *HypothesisService) GetCluster(ctx context.Context, ownerID, id string) (*commonv1.Cluster, error) {
	c, err := s.cat.GetCluster(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.GetOwnerId() != ownerID {
		return nil, ErrForbidden
	}
	return c, nil
}

func (s *HypothesisService) UpdateCluster(ctx context.Context, ownerID string, cluster *commonv1.Cluster) error {
	if _, err := s.GetCluster(ctx, ownerID, cluster.GetId()); err != nil {
		return err
	}
	cluster.OwnerId = ownerID
	return s.cat.UpdateCluster(ctx, cluster)
}

// DeleteAllClusters — только для ручной чистки оператором: автоматическая перекластеризация идёт версионной публикацией.
func (s *HypothesisService) DeleteAllClusters(ctx context.Context, ownerID string) error {
	return s.cat.DeleteClusters(ctx, ownerID)
}

func (s *HypothesisService) CreateHypothesis(
	ctx context.Context, ownerID string, req *dbv1.CreateHypothesisRequest,
) (*commonv1.Hypothesis, error) {
	if req.GetHypothesis() != nil {
		req.Hypothesis.OwnerId = ownerID
		s.applyRanking(ctx, req.Hypothesis)
	}
	return s.cat.CreateHypothesis(ctx, req)
}

func (s *HypothesisService) ListHypotheses(
	ctx context.Context, ownerID string, privileged bool, req *dbv1.ListHypothesesRequest,
) ([]*commonv1.Hypothesis, error) {
	req.OwnerId = ownerID
	if privileged {
		req.OwnerId = ""
	}
	return s.cat.ListHypotheses(ctx, req)
}

func (s *HypothesisService) GetHypothesis(
	ctx context.Context, ownerID string, privileged bool, id string,
) (*commonv1.Hypothesis, error) {
	h, err := s.cat.GetHypothesis(ctx, id)
	if err != nil {
		return nil, err
	}
	if !privileged && h.GetOwnerId() != ownerID {
		return nil, ErrForbidden
	}
	return h, nil
}

func (s *HypothesisService) UpdateHypothesis(
	ctx context.Context, ownerID string, req *dbv1.UpdateHypothesisRequest,
) error {
	if _, err := s.GetHypothesis(ctx, ownerID, false, req.GetHypothesis().GetId()); err != nil {
		return err
	}
	req.Hypothesis.OwnerId = ownerID
	// PUT может сменить оценки, TRL, свидетельства и привязку к KPI — композит пересчитывается, клиентскому не верим.
	s.applyRanking(ctx, req.Hypothesis)
	return s.cat.UpdateHypothesis(ctx, req)
}

func (s *HypothesisService) AddRevision(
	ctx context.Context, ownerID string, rev *commonv1.HypothesisRevision,
) (*commonv1.HypothesisRevision, error) {
	if _, err := s.GetHypothesis(ctx, ownerID, false, rev.GetHypothesisId()); err != nil {
		return nil, err
	}
	return s.cat.AddHypothesisRevision(ctx, rev)
}

func (s *HypothesisService) ListRevisions(
	ctx context.Context, ownerID string, privileged bool, hypothesisID string,
) ([]*commonv1.HypothesisRevision, error) {
	if _, err := s.GetHypothesis(ctx, ownerID, privileged, hypothesisID); err != nil {
		return nil, err
	}
	return s.cat.ListHypothesisRevisions(ctx, hypothesisID)
}

func (s *HypothesisService) ListEvidence(
	ctx context.Context, ownerID string, privileged bool, hypothesisID string,
) ([]*commonv1.HypothesisEvidence, error) {
	if _, err := s.GetHypothesis(ctx, ownerID, privileged, hypothesisID); err != nil {
		return nil, err
	}
	return s.cat.ListHypothesisEvidence(ctx, hypothesisID)
}

func (s *HypothesisService) HypothesisBoard(
	ctx context.Context,
	owner string,
	privileged bool,
	req *dbv1.ListHypothesesRequest,
) (*dbv1.ListHypothesesResponse, error) {
	req.OwnerId = owner
	if privileged {
		req.OwnerId = ""
	}
	req.Board = true
	req.Compact = true
	return s.cat.HypothesisBoard(ctx, req)
}
