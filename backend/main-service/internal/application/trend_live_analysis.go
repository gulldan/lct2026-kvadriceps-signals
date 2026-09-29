package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const LiveAnalysisVersion = "live-analysis-v1"

// LivePaperMetadata — версия работы на момент среза: более позднее наблюдение OpenAlex не доказывает аффилиацию на ранний срез.
type LivePaperMetadata struct {
	Abstract             string             `json:"abstract,omitempty"`
	Language             string             `json:"language,omitempty"`
	LanguageBasis        string             `json:"language_basis,omitempty"`
	Digest               string             `json:"digest,omitempty"`
	DigestModel          string             `json:"digest_model,omitempty"`
	PublisherType        string             `json:"publisher_type,omitempty"`
	Genre                string             `json:"genre,omitempty"`
	Trust                string             `json:"trust,omitempty"`
	TrustBasis           string             `json:"trust_basis,omitempty"`
	DOI                  string             `json:"doi,omitempty"`
	Category             string             `json:"category,omitempty"`
	AvailableAt          string             `json:"available_at,omitempty"`
	ObservedAt           string             `json:"observed_at,omitempty"`
	SourceURL            string             `json:"source_url,omitempty"`
	ContentHash          string             `json:"content_hash,omitempty"`
	Authorships          []LiveAuthorship   `json:"authorships,omitempty"`
	Links                []LiveEvidenceLink `json:"links,omitempty"`
	AffiliationCheckedAt string             `json:"affiliation_checked_at,omitempty"`
	AffiliationStatus    string             `json:"affiliation_status,omitempty"`
	AffiliationHash      string             `json:"affiliation_hash,omitempty"`
	CitedBy              *int               `json:"cited_by,omitempty"`
	CitedAt              string             `json:"cited_at,omitempty"`
	LineageCheckedAt     string             `json:"lineage_checked_at,omitempty"`
	LineageStatus        string             `json:"lineage_status,omitempty"`
	LineageHash          string             `json:"lineage_hash,omitempty"`

	SourceRevision *LivePublisherRevision `json:"source_revision,omitempty"`
}

// LiveAuthorship — утверждение провайдера с разрешимыми идентификаторами, а не совпадение имени автора.
type LiveAuthorship struct {
	AuthorID         string `json:"author_id"`
	AuthorName       string `json:"author_name"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	SourceURL        string `json:"source_url"`
	ObservedAt       string `json:"observed_at"`
}

type LiveEvidenceLink struct {
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	ParentURL  string `json:"parent_url,omitempty"`
	SourceURL  string `json:"source_url"`
	SourceHash string `json:"source_hash,omitempty"`
	ObservedAt string `json:"observed_at"`
	// Independent ставится только явным проверенным решением со своим источником: разные адреса, организации
	// или владельцы GitHub его не выставляют.
	Independent *bool `json:"independent"`
}

type LiveLineageEvent struct {
	Kind       string   `json:"kind"`
	At         string   `json:"at"`
	DateBasis  string   `json:"date_basis,omitempty"`
	PreviousAt string   `json:"previous_at,omitempty"`
	Parents    []string `json:"parents,omitempty"`
	OldName    string   `json:"old_name,omitempty"`
	Name       string   `json:"name"`
}

type LiveDiscovery struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Query            LiveQuery          `json:"query"`
	PaperIDs         []string           `json:"paper_ids"`
	Aliases          []string           `json:"aliases"`
	History          []LiveLineageEvent `json:"history"`
	Method           string             `json:"method"`
	Model            string             `json:"model"`
	Revision         string             `json:"revision"`
	Sampled          bool               `json:"sampled"`
	Coherence        float64            `json:"coherence"`
	ProfileBasis     string             `json:"profile_basis"`
	MissingAbstracts int                `json:"missing_abstracts"`
	JoinThreshold    float64            `json:"join_threshold"`
	SeedThreshold    float64            `json:"seed_threshold"`
	QualityStatus    string             `json:"quality_status"`
}

type LiveTopicAnalysis struct {
	Version   string           `json:"version"`
	Discovery *LiveDiscovery   `json:"discovery"`
	Evidence  LiveIndependence `json:"evidence"`
	Burst     LiveBurst        `json:"burst"`
}

// LiveAnalysisRequest несёт всё окно DiscoveryPapers, а не только работы тем; пустое окно — обогащение без поиска направлений.
type LiveAnalysisRequest struct {
	Snapshot           LiveSnapshot                 `json:"snapshot"`
	DiscoveryPapers    []StudyPaper                 `json:"discovery_papers,omitempty"`
	Metadata           map[string]LivePaperMetadata `json:"metadata,omitempty"`
	Previous           []LiveDiscovery              `json:"previous,omitempty"`
	PreviousCategories string                       `json:"previous_categories,omitempty"`
	StrictAvailability bool                         `json:"strict_availability"`
	Discover           bool                         `json:"discover"`
	DiscoveryTotal     int                          `json:"discovery_total"`
	EnrichMetadata     bool                         `json:"enrich_metadata"`
	EvidenceOnly       bool                         `json:"evidence_only"`
	EvidenceObservedAt string                       `json:"evidence_observed_at,omitempty"`
	Query              string                       `json:"query,omitempty"`
}

type LiveAnalysisResult struct {
	Version     string                       `json:"version"`
	Discoveries []LiveDiscovery              `json:"discoveries"`
	Topics      map[string]LiveTopicAnalysis `json:"topics"`
	Semantics   map[string]*LiveSemantic     `json:"semantics"`
	Limits      []LiveLimit                  `json:"limits"`
	Metadata    map[string]LivePaperMetadata `json:"metadata,omitempty"`
	Evidence    map[string]LiveIndependence  `json:"evidence,omitempty"`
}

// LiveSemanticEncoder используется и отдельным процессом radar-analysis.
type LiveSemanticEncoder = liveSemanticEncoder

// AnalyzeLive ничего не пишет: сборщик — единственный писатель и при отказе сохраняет последний удачный снимок.
func AnalyzeLive(ctx context.Context, encoder LiveSemanticEncoder, req LiveAnalysisRequest) (LiveAnalysisResult, error) {
	result := LiveAnalysisResult{Version: LiveAnalysisVersion, Discoveries: []LiveDiscovery{},
		Topics: map[string]LiveTopicAnalysis{}, Semantics: map[string]*LiveSemantic{}, Limits: []LiveLimit{}}
	cutoff, evidenceCutoff, err := liveAnalysisCutoffs(req)
	if err != nil {
		return result, err
	}
	if req.EvidenceOnly {
		result.Evidence = map[string]LiveIndependence{}
		for _, topic := range slices.Concat(req.Snapshot.Topics, req.Snapshot.Discovered) {
			result.Evidence[topic.ID] = liveIndependence(topic, req.Snapshot.Papers, req.Metadata, cutoff, evidenceCutoff)
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("analyze live evidence: %w", err)
		}
		return result, nil
	}
	if req.Discover {
		if encoder == nil {
			return result, errors.New("semantic discovery requires the configured model")
		}
		var err error
		result.Discoveries, result.Limits, err = liveSemanticDiscover(ctx, encoder, req)
		if err != nil {
			return result, err
		}
	}
	papers := liveUniquePapers(slices.Concat(req.Snapshot.Papers, req.DiscoveryPapers))
	all := liveAnalysisTopics(req, result.Discoveries, papers)
	for _, topic := range all {
		result.Topics[topic.ID] = liveAnalyzeTopic(topic, req, result.Discoveries, papers, cutoff, evidenceCutoff)
	}
	if encoder != nil {
		snapshot := req.Snapshot
		snapshot.Topics, snapshot.Discovered, snapshot.Papers = all, nil, papers
		s := &TrendLiveService{semantic: encoder}
		if err := s.annotateSemanticMetadata(ctx, &snapshot, req.Query, req.Metadata); err != nil {
			return result, err
		}
		for _, topic := range snapshot.Topics {
			result.Semantics[topic.ID] = topic.Semantic
		}
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("analyze live: %w", err)
	}
	return result, nil
}

func liveAnalysisCutoffs(req LiveAnalysisRequest) (string, string, error) {
	if len(req.Snapshot.Periods) == 0 || len(req.Snapshot.Periods) > 36 {
		return "", "", errors.New("analysis requires between 1 and 36 dated periods")
	}
	cutoff := req.Snapshot.Periods[len(req.Snapshot.Periods)-1].EndExclusive
	if _, ok := liveAnalysisDate(cutoff); !ok {
		return "", "", errors.New("analysis requires an explicit exclusive cutoff date")
	}
	evidenceCutoff := cutoff
	if req.EvidenceObservedAt != "" && !req.StrictAvailability {
		if _, err := time.Parse(time.RFC3339Nano, req.EvidenceObservedAt); err != nil {
			return "", "", errors.New("evidence observation requires an RFC3339 timestamp")
		}
		evidenceCutoff = req.EvidenceObservedAt
	}
	return cutoff, evidenceCutoff, nil
}

func liveAnalysisTopics(req LiveAnalysisRequest, discoveries []LiveDiscovery, papers []StudyPaper) []LiveTopic {
	all := slices.Clone(req.Snapshot.Topics)
	if !req.Discover {
		all = append(all, req.Snapshot.Discovered...)
	}
	for _, discovery := range discoveries {
		members := []StudyPaper{}
		for _, paper := range papers {
			if slices.Contains(discovery.PaperIDs, paper.ID) {
				members = append(members, paper)
			}
		}
		all = append(all, LiveTopic{ID: discovery.ID, Name: discovery.Name, PaperIDs: discovery.PaperIDs,
			Series: liveSeries(req.Snapshot.Periods, members)})
	}
	return all
}

func liveAnalyzeTopic(
	topic LiveTopic, req LiveAnalysisRequest, discoveries []LiveDiscovery, papers []StudyPaper, cutoff, evidenceCutoff string,
) LiveTopicAnalysis {
	analysis := LiveTopicAnalysis{Version: LiveAnalysisVersion,
		Evidence: liveIndependence(topic, papers, req.Metadata, cutoff, evidenceCutoff),
		Burst: liveBurst(topic, req.Snapshot.Periods, req.Snapshot.Categories,
			req.PreviousCategories, req.Snapshot.Limits)}
	if topic.Analysis != nil {
		analysis.Discovery = liveCorrectLegacyCreated(topic, req.Snapshot, req.StrictAvailability)
	}
	for _, discovery := range discoveries {
		if topic.ID == discovery.ID {
			analysis.Discovery = new(discovery)
		}
	}
	if analysis.Discovery != nil && analysis.Discovery.Sampled {
		analysis.Burst = LiveBurst{Method: liveBurstMethod, Status: "sampled_discovery",
			Flag: nil, QualityStatus: liveQualityDevelopment,
			DetectionTarget: liveBurstTarget, Role: "descriptive_screening"}
	}
	return analysis
}

func liveUniquePapers(papers []StudyPaper) []StudyPaper {
	seen := map[string]bool{}
	result := make([]StudyPaper, 0, len(papers))
	for _, paper := range papers {
		if paper.ID != "" && !seen[paper.ID] && strings.TrimSpace(paper.Title) != "" {
			seen[paper.ID] = true
			result = append(result, paper)
		}
	}
	return result
}

// PrepareLiveAnalysisRequest режет окно тем же бюджетом до передачи, иначе полное окно arXiv раздувает тело JSON.
func PrepareLiveAnalysisRequest(req LiveAnalysisRequest) LiveAnalysisRequest {
	if req.Metadata == nil {
		req.Metadata = req.Snapshot.PaperMetadata
	}
	if req.Discover && len(req.Snapshot.Periods) > 0 {
		eligible, _ := liveEligibleDiscovery(req)
		req.DiscoveryTotal = max(req.DiscoveryTotal, len(eligible))
		req.DiscoveryPapers = liveStratifiedPapers(eligible, req.Metadata, liveSemanticDiscoveryLimit)
	}
	keep := map[string]bool{}
	for _, paper := range slices.Concat(req.Snapshot.Papers, req.DiscoveryPapers) {
		keep[paper.ID] = true
	}
	for _, topic := range slices.Concat(req.Snapshot.Topics, req.Snapshot.Discovered) {
		for _, event := range topic.External {
			keep[event.URL] = true
		}
		for _, channel := range []*LiveChannel{topic.Patents, topic.Web} {
			if channel != nil {
				for _, record := range channel.Records {
					keep[record.ID] = true
				}
			}
		}
	}
	metadata := map[string]LivePaperMetadata{}
	for id := range keep {
		if value, ok := req.Metadata[id]; ok {
			metadata[id] = value
		}
	}
	req.Metadata, req.Snapshot.PaperMetadata = metadata, nil
	return req
}
