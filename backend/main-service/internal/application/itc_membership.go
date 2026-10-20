package application

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const itcInputOrigin = "input"

func itcEvidenceDocuments(evidence []*commonv1.HypothesisEvidence) map[string]struct{} {
	documents := make(map[string]struct{})
	classified := false
	for _, e := range evidence {
		if e.GetDocumentId() == "" || strings.TrimSpace(e.GetSnippet()) == "" || e.GetOrigin() == itcInputOrigin {
			continue
		}
		documents[e.GetDocumentId()] = struct{}{}
		if e.GetStance() == "supports" || e.GetStance() == "contradicts" {
			classified = true
		}
	}
	if !classified {
		return nil
	}
	return documents
}

func (s *HypothesisService) documentBackedITC(
	ctx context.Context, ownerID string, h *commonv1.Hypothesis, clusterID string,
) ([]byte, error) {
	documents := itcEvidenceDocuments(h.GetEvidence())
	if clusterID == "" || len(documents) == 0 {
		return nil, fmt.Errorf("%w: topic association requires cited documents", ErrInvalidArgument)
	}
	// Текущая публикация исключает старые темы и неоднозначное членство после пересборки.
	clusters, err := s.ListClusters(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	selectedID := ""
	var selectedITC jsontext.Value
	for _, c := range clusters {
		var params struct {
			Members []string       `json:"members"`
			ITC     jsontext.Value `json:"itc"`
		}
		if json.Unmarshal([]byte(c.GetParams()), &params) != nil {
			continue
		}
		matches := true
		for document := range documents {
			if !slices.Contains(params.Members, document) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if selectedID != "" {
			return nil, fmt.Errorf("%w: cited documents belong to multiple topics", ErrInvalidArgument)
		}
		selectedID, selectedITC = c.GetId(), params.ITC
	}
	if selectedID != clusterID {
		return nil, fmt.Errorf("%w: topic membership is not supported by cited documents", ErrInvalidArgument)
	}
	band := itcBand([]byte(selectedITC))
	if band < 1 || band > 10 {
		return nil, fmt.Errorf("%w: associated topic has no valid ITC", ErrInvalidArgument)
	}
	var itc map[string]any
	if decodeErr := json.Unmarshal(selectedITC, &itc); decodeErr != nil {
		return nil, fmt.Errorf("%w: invalid topic ITC", ErrInvalidArgument)
	}
	itc["scope"] = "hypothesis(document_membership)"
	itc["association"] = map[string]any{
		"cluster_id": clusterID, "document_ids": slices.Sorted(maps.Keys(documents)),
	}
	raw, err := json.Marshal(itc)
	if err != nil {
		return nil, fmt.Errorf("encode document-backed topic ITC: %w", err)
	}
	return raw, nil
}
