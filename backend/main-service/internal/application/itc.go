package application

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

//go:embed itc_rubric.json
var itcRubricRaw []byte

func (s *HypothesisService) ITCRubric() []byte { return itcRubricRaw }

// StoreClusterITC перечитывает кластер, чтобы не затереть состав, представителей и метаданные.
func (s *HypothesisService) StoreClusterITC(
	ctx context.Context, ownerID, id string, itc json.RawMessage,
) (*commonv1.Cluster, error) {
	if band := itcBand(itc); !json.Valid(itc) || band < 1 || band > 10 {
		return nil, fmt.Errorf("%w: ITC score must be between 1 and 10", ErrInvalidArgument)
	}
	c, err := s.GetCluster(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	c.Params = mergeITC(c.GetParams(), itc)
	if err := s.cat.UpdateCluster(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ITCInput: Composite принимается ради совместимости формата, но игнорируется.
type ITCInput struct {
	ClusterID string          `json:"cluster_id"`
	ITC       json.RawMessage `json:"itc"`
	Composite *float64        `json:"composite_score"`
}

// StoreITC сохраняет индекс связанной темы после проверки документов. Строка перечитывается вместе
// с идентификаторами доказательств, чтобы обновление их не дублировало.
func (s *HypothesisService) StoreITC(ctx context.Context, ownerID, id string, in ITCInput) (*commonv1.Hypothesis, error) {
	h, err := s.GetHypothesis(ctx, ownerID, false, id)
	if err != nil {
		return nil, err
	}
	itc, err := s.documentBackedITC(ctx, ownerID, h, in.ClusterID)
	if err != nil {
		return nil, err
	}
	h.PrimaryClusterId = new(in.ClusterID)
	// in.Composite намеренно не применяется: composite_score считает только ранжировщик rank-v1.
	h.Assessment = mergeITC(h.GetAssessment(), itc)
	rev := &commonv1.HypothesisRevision{
		Action: actionScoreOverride, EditorId: editorSystem,
		Summary: fmt.Sprintf("Индекс связанной темы: %d/10", itcBand(itc)),
	}
	if uerr := s.cat.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		return nil, uerr
	}
	return s.GetHypothesis(ctx, ownerID, false, id)
}

func mergeITC(assessment string, itc json.RawMessage) string {
	m := map[string]any{}
	if assessment != "" {
		_ = json.Unmarshal([]byte(assessment), &m)
	}
	var v any
	if len(itc) > 0 && json.Valid(itc) {
		_ = json.Unmarshal(itc, &v)
	}
	m["itc"] = v
	b, err := json.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(b)
}

func itcBand(itc json.RawMessage) int {
	var v struct {
		Score int `json:"score"`
	}
	_ = json.Unmarshal(itc, &v)
	return v.Score
}
