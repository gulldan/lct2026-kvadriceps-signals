// Граф свидетельств намеренно отделён от типизированного графа знаний в kg.go. Строится из существующих
// чтений без нового запроса — для портфеля размером с доску этого хватает.

package application

import (
	"context"
	"encoding/json"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"

	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

// graphMaxHypotheses — страховка, а не лимит реального портфеля.
const graphMaxHypotheses = 1000

type GraphNode struct {
	ID      string // id гипотезы или документа; "file:<имя>", если id нет
	Kind    string // "hypothesis" | "document"
	Label   string
	Status  string
	TRL     *int32
	Verdict string
	Degree  int
	KPIID   string
}

type GraphEdge struct {
	Source  string // id гипотезы
	Target  string // id узла документа
	Class   string // supports | contradicts | context
	Score   *float64
	ChunkID string
}

type HypothesisGraph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

func (s *HypothesisService) Graph(ctx context.Context, ownerID string) (*HypothesisGraph, error) {
	hyps := make([]*commonv1.Hypothesis, 0, graphMaxHypotheses)
	err := s.walkHypotheses(ctx, &dbv1.ListHypothesesRequest{OwnerId: ownerID}, func(page []*commonv1.Hypothesis) (bool, error) {
		hyps = append(hyps, page[:min(len(page), graphMaxHypotheses-len(hyps))]...)
		return len(hyps) < graphMaxHypotheses, nil
	})
	if err != nil {
		return nil, err
	}

	g := &HypothesisGraph{}
	docDegree := make(map[string]int)
	docLabel := make(map[string]string)
	for _, h := range hyps {
		g.Nodes = append(g.Nodes, GraphNode{
			ID: h.GetId(), Kind: "hypothesis", Label: h.GetTitle(),
			Status: h.GetStatus(), TRL: h.Trl, Verdict: checkVerdict(h.GetAssessment()),
			KPIID: h.GetKpiId(),
		})
		evs, eerr := s.cat.ListHypothesisEvidence(ctx, h.GetId())
		if eerr != nil {
			continue // гипотеза с нечитаемыми свидетельствами всё равно остаётся узлом
		}
		for _, e := range evs {
			docID := e.GetDocumentId()
			if docID == "" {
				if e.GetFilename() == "" {
					continue
				}
				docID = "file:" + e.GetFilename()
			}
			if _, seen := docLabel[docID]; !seen {
				docLabel[docID] = e.GetFilename()
			}
			docDegree[docID]++
			score := e.Score
			g.Edges = append(g.Edges, GraphEdge{
				Source: h.GetId(), Target: docID, Class: e.GetStance(),
				Score: score, ChunkID: e.GetChunkId(),
			})
		}
	}
	for id, deg := range docDegree {
		label := docLabel[id]
		if label == "" {
			label = id
		}
		g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: "document", Label: label, Degree: deg})
	}
	return g, nil
}

func checkVerdict(assessment string) string {
	if assessment == "" {
		return ""
	}
	var m struct {
		Check struct {
			Verdict string `json:"verdict"`
		} `json:"check"`
	}
	if err := json.Unmarshal([]byte(assessment), &m); err != nil {
		return ""
	}
	return m.Check.Verdict
}
