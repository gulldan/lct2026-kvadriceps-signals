package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

type LiveAnalyzer interface {
	Analyze(context.Context, LiveAnalysisRequest) (LiveAnalysisResult, error)
}

func (s *TrendLiveService) SetAnalyzer(analyzer LiveAnalyzer) { s.analyzer = analyzer }

func (s *TrendLiveService) previousAnalysis() ([]LiveDiscovery, string, map[string]LivePaperMetadata) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return nil, "", nil
	}
	previous := []LiveDiscovery{}
	for _, topic := range s.snapshot.Discovered {
		if topic.Analysis != nil && topic.Analysis.Discovery != nil {
			previous = append(previous, *liveCorrectLegacyCreated(topic, *s.snapshot, false))
		} else {
			previous = append(previous, LiveDiscovery{ID: topic.ID, Name: topic.Name, Query: topic.Query,
				PaperIDs: slices.Clone(topic.PaperIDs), Aliases: []string{topic.Name}, History: []LiveLineageEvent{}})
		}
	}
	return previous, s.snapshot.Categories, s.snapshot.PaperMetadata
}

func liveMergeMetadata(current, previous map[string]LivePaperMetadata, papers []StudyPaper) map[string]LivePaperMetadata {
	result := map[string]LivePaperMetadata{}
	for _, paper := range papers {
		value, exists := current[paper.ID]
		old := previous[paper.ID]
		if !exists {
			value = old
		}
		if value.CitedAt == "" && old.CitedAt != "" {
			value.CitedBy, value.CitedAt = old.CitedBy, old.CitedAt
		}
		if value.AffiliationCheckedAt == "" {
			value.Authorships = old.Authorships
			value.AffiliationCheckedAt = old.AffiliationCheckedAt
			value.AffiliationStatus = old.AffiliationStatus
			value.AffiliationHash = old.AffiliationHash
		}
		result[paper.ID] = value
	}
	// Кэш проверенных ссылок переживает научные обновления; лишние ссылки транспорт потом отсекает.
	for id, value := range previous {
		if strings.HasPrefix(id, "https://github.com/") && value.LineageCheckedAt != "" {
			result[id] = value
		}
	}
	return result
}

func liveApplyAnalysis(snapshot *LiveSnapshot, result LiveAnalysisResult) {
	for _, topics := range [][]LiveTopic{snapshot.Topics, snapshot.Discovered} {
		for i := range topics {
			if analysis, ok := result.Topics[topics[i].ID]; ok {
				if analysis.Discovery == nil && topics[i].Analysis != nil {
					analysis.Discovery = topics[i].Analysis.Discovery
				}
				topics[i].Analysis = new(analysis)
			}
			if semantic, ok := result.Semantics[topics[i].ID]; ok {
				topics[i].Semantic = semantic
			}
		}
	}
	if result.Metadata != nil {
		snapshot.PaperMetadata = result.Metadata
		snapshot.Limits = slices.DeleteFunc(slices.Clone(snapshot.Limits), func(limit LiveLimit) bool {
			return strings.HasPrefix(limit.Text, "affiliations:") || strings.HasPrefix(limit.Text, "github lineage:")
		})
	}
	snapshot.Limits = append(snapshot.Limits, result.Limits...)
}

func (s *TrendLiveService) enrichAnalysis(ctx context.Context, snapshot *LiveSnapshot, query string, enrichMetadata bool) error {
	if s.analyzer == nil {
		return s.annotateSemanticMetadata(ctx, snapshot, query, snapshot.PaperMetadata)
	}
	previous, categories, _ := s.previousAnalysis()
	started := time.Now()
	result, err := s.analyzer.Analyze(ctx, LiveAnalysisRequest{Snapshot: *snapshot, Metadata: snapshot.PaperMetadata,
		Previous: previous, PreviousCategories: categories, Query: query, EnrichMetadata: enrichMetadata})
	papers, chars := liveAnalysisSize(snapshot)
	liveTallyOf(ctx).addAnalysis(papers, chars, time.Since(started))
	if err != nil {
		return fmt.Errorf("live analysis: %w", err)
	}
	liveApplyAnalysis(snapshot, result)
	return nil
}

func (s *TrendLiveService) enrichSourceEvidence(ctx context.Context, snapshot *LiveSnapshot) error {
	if s.analyzer == nil {
		return nil
	}
	needsEvidence := false
	for _, topics := range [][]LiveTopic{snapshot.Topics, snapshot.Discovered} {
		if slices.ContainsFunc(topics, func(t LiveTopic) bool { return t.Analysis != nil }) {
			needsEvidence = true
		}
	}
	if !needsEvidence {
		return nil
	}
	result, err := s.analyzer.Analyze(ctx, LiveAnalysisRequest{
		Snapshot: *snapshot, Metadata: snapshot.PaperMetadata, EvidenceOnly: true,
	})
	if err != nil {
		return fmt.Errorf("source evidence analysis: %w", err)
	}
	for _, topics := range [][]LiveTopic{snapshot.Topics, snapshot.Discovered} {
		for i := range topics {
			if topics[i].Analysis == nil {
				continue
			}
			evidence, ok := result.Evidence[topics[i].ID]
			if !ok {
				return fmt.Errorf("source evidence analysis missing topic %q", topics[i].ID)
			}
			analysis := *topics[i].Analysis
			analysis.Evidence = evidence
			topics[i].Analysis = new(analysis)
		}
	}
	return nil
}

// RefreshAnalysis не проходит arXiv заново; снимок защищают аренда писателя и условная публикация.
func (s *TrendLiveService) RefreshAnalysis(ctx context.Context) error {
	if s.analyzer == nil {
		return errors.New("radar analysis service is not configured")
	}
	ctx, finish, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer finish()
	s.mu.RLock()
	if s.snapshot == nil {
		s.mu.RUnlock()
		return ErrLiveNotReady
	}
	raw, err := json.Marshal(s.snapshot)
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("encode live snapshot for analysis: %w", err)
	}
	var snapshot LiveSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("decode live snapshot for analysis: %w", err)
	}
	if err := s.enrichAnalysis(ctx, &snapshot, "", true); err != nil {
		return err
	}
	s.describeTopics(ctx, &snapshot)
	snapshot.Method = liveMethod
	return s.publish(ctx, &snapshot)
}

func liveSearchMetadata(snapshot *LiveSnapshot, src *liveSources, papers []StudyPaper) map[string]LivePaperMetadata {
	metadata := maps.Clone(snapshot.PaperMetadata)
	if metadata == nil {
		metadata = map[string]LivePaperMetadata{}
	}
	for _, paper := range papers {
		if value, ok := src.metadata[paper.ID]; ok {
			metadata[paper.ID] = value
		}
	}
	return metadata
}
