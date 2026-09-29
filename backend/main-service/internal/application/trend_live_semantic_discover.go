package application

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	liveSemanticDiscoveryMethod = "dated-semantic-leader-v3"
	liveSemanticDiscoveryLimit  = 12288
	liveSemanticClusters        = liveSemanticDiscoveryLimit
	liveSemanticJoin            = .75
	liveSemanticSeedJoin        = .72
)

type liveSemanticCluster struct {
	seed, centroid, sum []float64
	rows                []int
}

// Состав групп решают эмбеддинги до извлечения фраз; фразы только называют группы.
func liveSemanticDiscover(
	ctx context.Context, encoder liveSemanticEncoder, req LiveAnalysisRequest,
) ([]LiveDiscovery, []LiveLimit, error) {
	papers, unknown := liveEligibleDiscovery(req)
	limits := []LiveLimit{}
	if unknown > 0 {
		limits = append(limits, liveLimitOf(liveLimitDiscoveryUndated, fmt.Sprintf(
			"discovery: %d paper versions have no availability timestamp; "+
				"publication dates used for this live observation", unknown),
			map[string]int{liveValuePapers: unknown}))
	}
	sampled := max(req.DiscoveryTotal, len(papers)) > liveSemanticDiscoveryLimit
	if sampled {
		limits = append(limits, liveLimitOf(liveLimitDiscoverySample, fmt.Sprintf(
			"discovery: candidates found on a stratified deterministic sample of %d of %d eligible papers; "+
				"each direction is recounted on the full window",
			min(liveSemanticDiscoveryLimit, len(papers)), max(req.DiscoveryTotal, len(papers))),
			map[string]int{
				"sampled":  min(liveSemanticDiscoveryLimit, len(papers)),
				"eligible": max(req.DiscoveryTotal, len(papers)),
			}))
		papers = liveBranchSample(papers, req.Metadata, liveSemanticDiscoveryLimit)
	}
	if len(papers) < 5 {
		return []LiveDiscovery{}, append(limits,
			liveLimitOf(liveLimitDiscoveryThin, "discovery: fewer than five eligible papers", nil)), nil
	}
	slices.SortFunc(papers, func(a, b StudyPaper) int { return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.ID, b.ID)) })
	batch, err := liveDiscoveryBatch(ctx, encoder, papers, req)
	if err != nil {
		return nil, limits, err
	}
	clusters, noise, err := liveSemanticGroup(ctx, batch.Vectors)
	if err != nil {
		return nil, limits, err
	}
	if noise > 0 {
		limits = append(limits, liveLimitOf(liveLimitDiscoveryNoise, fmt.Sprintf(
			"discovery: %d papers did not join the bounded set of %d clusters", noise, liveSemanticClusters),
			map[string]int{liveValuePapers: noise, "clusters": liveSemanticClusters}))
	}
	candidates := liveDiscoveryCandidates(clusters, papers, req, batch, sampled)
	result := liveSelectDiscoveries(candidates)
	liveDiscoveryLabels(result, papers)
	at, basis := liveLineageDate(req)
	liveDiscoveryLineage(result, req.Previous, at, basis)
	return result, limits, nil
}

func liveDiscoveryBatch(
	ctx context.Context, encoder liveSemanticEncoder, papers []StudyPaper, req LiveAnalysisRequest,
) (LiveEmbeddingBatch, error) {
	texts := make([]string, len(papers))
	for i, paper := range papers {
		meta := req.Metadata[paper.ID]
		if !liveBeforeCutoff(meta.AvailableAt, req.Snapshot.Periods[len(req.Snapshot.Periods)-1].EndExclusive) {
			meta.Abstract = ""
		}
		texts[i] = liveSemanticText(paper.Title, meta.Abstract)
	}
	batch, err := encoder.Encode(ctx, texts)
	if err != nil {
		return LiveEmbeddingBatch{}, fmt.Errorf("semantic discovery: %w", err)
	}
	if batch.Model == "" || batch.Revision == "" || len(batch.Vectors) != len(papers) || len(batch.Vectors[0]) == 0 {
		return LiveEmbeddingBatch{}, errors.New("semantic discovery: incompatible model response")
	}
	dimension := len(batch.Vectors[0])
	for i, vector := range batch.Vectors {
		unit, ok := liveUnitVector(vector)
		if !ok || len(unit) != dimension {
			return LiveEmbeddingBatch{}, errors.New("semantic discovery: invalid embedding")
		}
		batch.Vectors[i] = unit
	}
	return batch, nil
}

type liveDiscoveryCandidate struct {
	discovery LiveDiscovery
	recent    int
	profile   []float64
	branch    string
}

func liveDiscoveryCandidates(
	clusters []liveSemanticCluster, papers []StudyPaper, req LiveAnalysisRequest, batch LiveEmbeddingBatch, sampled bool,
) []liveDiscoveryCandidate {
	global := map[string]int{}
	for _, paper := range papers {
		for _, gram := range liveGrams(liveTokens(paper.Title), nil) {
			global[gram.key]++
		}
	}
	candidates := []liveDiscoveryCandidate{}
	curated := map[string]bool{}
	for _, topic := range req.Snapshot.Topics {
		for _, id := range topic.PaperIDs {
			curated[id] = true
		}
	}
	for _, cluster := range clusters {
		if candidate, ok := liveClusterDiscovery(cluster, papers, req, batch, global, curated, sampled); ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func liveClusterDiscovery(
	cluster liveSemanticCluster, papers []StudyPaper, req LiveAnalysisRequest, batch LiveEmbeddingBatch,
	global map[string]int, curated map[string]bool, sampled bool,
) (liveDiscoveryCandidate, bool) {
	members := make([]StudyPaper, 0, len(cluster.rows))
	ids, overlap := make([]string, 0, len(cluster.rows)), 0
	coherence := 1.0
	missingAbstracts := 0
	for _, row := range cluster.rows {
		paper := papers[row]
		members, ids = append(members, paper), append(ids, paper.ID)
		if curated[paper.ID] {
			overlap++
		}
		coherence = min(coherence, liveSimilarity(cluster.centroid, batch.Vectors[row]))
		meta := req.Metadata[paper.ID]
		if strings.TrimSpace(meta.Abstract) == "" ||
			!liveBeforeCutoff(meta.AvailableAt, req.Snapshot.Periods[len(req.Snapshot.Periods)-1].EndExclusive) {
			missingAbstracts++
		}
	}
	comparison := liveCompare(req.Snapshot.Periods, members)
	if comparison == nil || comparison.Current < 5 || comparison.Prior > max(2, comparison.Current/3) ||
		float64(overlap) >= .8*float64(len(members)) {
		return liveDiscoveryCandidate{}, false
	}
	aliases := liveSemanticNames(members, global, len(papers))
	name := aliases[0]
	seed := sha256.Sum256([]byte(liveSemanticDiscoveryMethod + ":" + members[0].ID))
	discovery := LiveDiscovery{ID: fmt.Sprintf("semantic:%x", seed[:8]), Name: name,
		PaperIDs: liveSortedUnique(ids), Aliases: aliases, History: []LiveLineageEvent{},
		Method: liveSemanticDiscoveryMethod, Model: batch.Model, Revision: batch.Revision,
		Sampled: sampled, Coherence: coherence, MissingAbstracts: missingAbstracts,
		ProfileBasis: liveProfileTitlesAndAbstracts, JoinThreshold: liveSemanticJoin,
		SeedThreshold: liveSemanticSeedJoin, QualityStatus: liveQualityDevelopment}
	if missingAbstracts == len(members) {
		discovery.ProfileBasis = "titles"
	}
	if missingAbstracts > 0 && missingAbstracts < len(members) {
		discovery.ProfileBasis = "mixed"
	}
	discovery.Query = liveDiscoveryQuery(aliases)
	return liveDiscoveryCandidate{discovery, comparison.Current, cluster.centroid,
		liveTopicBranch(members, req.Metadata)}, true
}

func liveSelectDiscoveries(candidates []liveDiscoveryCandidate) []LiveDiscovery {
	slices.SortFunc(candidates, func(a, b liveDiscoveryCandidate) int {
		return cmp.Or(cmp.Compare(b.recent, a.recent), cmp.Compare(a.discovery.ID, b.discovery.ID))
	})
	result, profiles := []LiveDiscovery{}, [][]float64{}
	perBranch := map[string]int{}
	for len(candidates) > 0 && len(result) < liveDiscoverMax {
		best, bestScore := -1, math.Inf(-1)
		for i, candidate := range candidates {
			if perBranch[candidate.branch] >= liveDiscoverPerBranch {
				continue
			}
			near := 0.0
			for _, profile := range profiles {
				near = max(near, liveSimilarity(candidate.profile, profile))
			}
			score := math.Log1p(float64(candidate.recent)) - 2*near
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		perBranch[candidates[best].branch]++
		result, profiles = append(result, candidates[best].discovery), append(profiles, candidates[best].profile)
		candidates = slices.Delete(candidates, best, best+1)
	}
	return result
}

func liveEligibleDiscovery(req LiveAnalysisRequest) ([]StudyPaper, int) {
	end := req.Snapshot.Periods[len(req.Snapshot.Periods)-1].EndExclusive
	start := req.Snapshot.Periods[0].Start
	result, unknown := []StudyPaper{}, 0
	for _, paper := range liveUniquePapers(req.DiscoveryPapers) {
		meta := req.Metadata[paper.ID]
		if !liveBeforeCutoff(paper.Date, end) || paper.Date < start {
			continue
		}
		if meta.AvailableAt != "" && !liveBeforeCutoff(meta.AvailableAt, end) {
			continue
		}
		if meta.AvailableAt == "" {
			if req.StrictAvailability {
				continue
			}
			unknown++
			// Работы неизвестной версии идут в живую диагностику, но без метки времени не входят в историческое измерение.
		}
		result = append(result, paper)
	}
	return result, unknown
}

func liveStratifiedPapers(papers []StudyPaper, metadata map[string]LivePaperMetadata, limit int) []StudyPaper {
	// Хеш — раз на работу: SHA-256 в каждом сравнении при сотне тысяч совпадений стоил дороже поиска.
	sums := make([][sha256.Size]byte, len(papers))
	groups := map[string][]int{}
	for i, paper := range papers {
		key := paper.Date[:7] + ":" + metadata[paper.ID].Category
		groups[key] = append(groups[key], i)
		sums[i] = sha256.Sum256([]byte(paper.ID))
	}
	keys := make([]string, 0, len(groups))
	for key, group := range groups {
		keys = append(keys, key)
		slices.SortFunc(group, func(a, b int) int { return bytes.Compare(sums[a][:], sums[b][:]) })
	}
	slices.Sort(keys)
	result := make([]StudyPaper, 0, limit)
	for row := 0; len(result) < limit; row++ {
		added := false
		for _, key := range keys {
			if row < len(groups[key]) {
				result = append(result, papers[groups[key][row]])
				added = true
				if len(result) == limit {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	return result
}

func liveSemanticGroup(ctx context.Context, vectors [][]float64) ([]liveSemanticCluster, int, error) {
	if len(vectors) > liveSemanticDiscoveryLimit {
		return nil, 0, errors.New("semantic grouping exceeds the bounded paper input")
	}
	clusters, noise := []liveSemanticCluster{}, 0
	// Вход уже ограничен: второй, меньший бюджет зёрен молча отбросил бы поздние работы.
	for row, vector := range vectors {
		if err := ctx.Err(); err != nil {
			return nil, noise, fmt.Errorf("semantic grouping: %w", err)
		}
		best, score := -1, liveSemanticJoin
		for i, cluster := range clusters {
			candidate := liveSimilarity(vector, cluster.centroid)
			if candidate >= score && liveSimilarity(vector, cluster.seed) >= liveSemanticSeedJoin {
				best, score = i, candidate
			}
		}
		if best < 0 {
			if len(clusters) >= liveSemanticClusters {
				noise++
				continue
			}
			clusters = append(clusters, liveSemanticCluster{
				seed: vector, centroid: vector, sum: slices.Clone(vector), rows: []int{row},
			})
			continue
		}
		cluster := &clusters[best]
		cluster.rows = append(cluster.rows, row)
		for d, value := range vector {
			cluster.sum[d] += value
		}
		cluster.centroid, _ = liveUnitVector(cluster.sum)
	}
	return clusters, noise, nil
}
