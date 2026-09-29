package analysisdoc

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"io"
	"math"
	"slices"

	"github.com/example/db-parser/internal/platform/indexdoc"
)

const diversityVersion = "source-centroid-mmr-v1-development"
const verifiedBodyDiversityVersion = "source-centroid-mmr-v2-verified-body"
const similarityScale = 1_000_000
const providerArxiv = "arxiv"

func validDiversityVersion(version string) bool {
	return version == diversityVersion || version == verifiedBodyDiversityVersion
}

func matchesDiversityMethod(version string, diversity *Diversity) bool {
	if version == "" {
		return diversity == nil
	}
	return validDiversityVersion(version) && diversity != nil && diversity.Version == version
}

func normalizeMean(vectors [][]float64, dimension int) ([]float64, error) {
	if len(vectors) == 0 || dimension < 1 || dimension > 32768 {
		return nil, ErrInvalid
	}
	mean := make([]float64, dimension)
	for _, vector := range vectors {
		if len(vector) != dimension {
			return nil, ErrInvalid
		}
		var nonzero bool
		for i, value := range vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, ErrInvalid
			}
			mean[i] += value / float64(len(vectors))
			nonzero = nonzero || value != 0
		}
		if !nonzero {
			return nil, ErrInvalid
		}
	}
	var norm float64
	for _, value := range mean {
		norm += value * value
	}
	norm = math.Sqrt(norm)
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, ErrInvalid
	}
	for i := range mean {
		mean[i] /= norm
	}
	return mean, nil
}

func snapshotVector(ctx context.Context, store ObjectReader, record Record, dimension int) ([]float64, int64, error) {
	if store == nil {
		return nil, 0, ErrInvalid
	}
	reader, err := store.Get(ctx, record.Index.Data.Key)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, (128<<20)+1))
	if err != nil || len(data) > 128<<20 || indexdoc.Hash(data) != record.Index.Data.SHA256 {
		return nil, int64(len(data)), ErrInvalid
	}
	var snapshot struct {
		DocumentID string         `json:"document_id"`
		SnapshotID string         `json:"snapshot_id"`
		Model      indexdoc.Model `json:"model"`
		Chunks     []struct {
			Vector []float64 `json:"vector"`
		} `json:"chunks"`
	}
	if json.Unmarshal(data, &snapshot) != nil || snapshot.DocumentID != record.ID ||
		snapshot.SnapshotID != record.Index.SnapshotID || snapshot.Model != record.Index.Model ||
		snapshot.Model.Dimension != dimension || len(snapshot.Chunks) != record.Index.ChunkCount ||
		len(snapshot.Chunks) == 0 || len(snapshot.Chunks) > 8192 {
		return nil, int64(len(data)), ErrInvalid
	}
	vectors := make([][]float64, len(snapshot.Chunks))
	for i, chunk := range snapshot.Chunks {
		vectors[i] = chunk.Vector
	}
	vector, err := normalizeMean(vectors, dimension)
	return vector, int64(len(data)), err
}

func representationRank(record Record, verifiedBody bool) int {
	if verifiedBody {
		switch record.Source.TextStatus {
		case "metadata_only", "title_only":
			return 3
		case "article_text", "pdf", "full_text":
			return 0
		}
		if record.Representation == "abstract" {
			return 1
		}
	}
	if record.Representation == "full_text" {
		return 0
	}
	if verifiedBody {
		return 2
	}
	return 1
}

func representative(work Work, records map[string]Record, verifiedBody bool) (Record, error) {
	var selected Record
	for _, id := range work.DocumentIDs {
		record, exists := records[id]
		if !exists {
			return Record{}, ErrInvalid
		}
		// Воспроизводим закреплённую версию метода: старые результаты сохраняют прежнее предпочтение.
		order := cmp.Compare(representationRank(record, verifiedBody), representationRank(selected, verifiedBody))
		if selected.ID == "" || order < 0 || order == 0 &&
			(record.Source.Provider == providerArxiv && selected.Source.Provider != providerArxiv ||
				(record.Source.Provider == providerArxiv) == (selected.Source.Provider == providerArxiv) &&
					record.ID < selected.ID) {
			selected = record
		}
	}
	if selected.ID == "" {
		return Record{}, ErrInvalid
	}
	return selected, nil
}

func indexByID[T any](items []T, id func(T) string) map[string]T {
	indexed := make(map[string]T, len(items))
	for _, item := range items {
		indexed[id(item)] = item
	}
	return indexed
}

func verifyDiversity(ctx context.Context, store ObjectReader, input Input, result Result, total int64) error {
	if result.Ranking.Diversity == nil { // старые неизменяемые версии метода по-прежнему проверяются
		return nil
	}
	records := indexByID(input.Records, func(record Record) string { return record.ID })
	works := indexByID(result.Works, func(work Work) string { return work.ID })
	candidates := indexByID(result.Candidates, func(candidate Candidate) string { return candidate.ID })
	pool := orderedCandidates(candidates)
	if len(pool) > 0 && input.Model == nil {
		return ErrInvalid
	}
	centers := make(map[string][]float64, len(pool))
	for _, id := range pool {
		memberIDs := slices.Clone(candidates[id].WorkIDs)
		slices.Sort(memberIDs)
		vectors := make([][]float64, 0, len(memberIDs))
		for _, workID := range memberIDs {
			record, err := representative(works[workID], records, result.Ranking.Diversity.Version == verifiedBodyDiversityVersion)
			if err != nil || record.Index.Model != *input.Model {
				return ErrInvalid
			}
			vector, bytes, err := snapshotVector(ctx, store, record, input.Model.Dimension)
			total += bytes
			if err != nil || total > MaxAnalysisArtifactBytes {
				return ErrInvalid
			}
			vectors = append(vectors, vector)
		}
		center, err := normalizeMean(vectors, input.Model.Dimension)
		if err != nil {
			return err
		}
		centers[id] = center
	}
	ordered, suppressed := diversify(pool, centers)
	if !slices.Equal(ordered, result.Ranking.OrderedIDs) ||
		!slices.Equal(suppressed, result.Ranking.Diversity.SuppressedIDs) {
		return ErrInvalid
	}
	return nil
}

func diversify(pool []string, centers map[string][]float64) ([]string, []string) {
	quality, redundancy := map[string]int{}, map[string]int{}
	for i, id := range pool {
		quality[id] = (len(pool) - i) * similarityScale / len(pool)
		redundancy[id] = 0
	}
	selected, suppressed := []string{}, []string{}
	for len(quality) > 0 && len(selected) < 15 {
		best, bestScore := "", -similarityScale*4
		for id, score := range quality {
			score = 3*score - redundancy[id]
			if score > bestScore || score == bestScore && id < best {
				best, bestScore = id, score
			}
		}
		selected = append(selected, best)
		delete(quality, best)
		for id := range quality {
			var cosine float64
			for i, value := range centers[id] {
				cosine += value * centers[best][i]
			}
			similarity := int(math.Floor(max(0, min(1, cosine))*similarityScale + .5))
			redundancy[id] = max(redundancy[id], similarity)
			if redundancy[id] >= 920_000 {
				suppressed = append(suppressed, id)
				delete(quality, id)
			}
		}
	}
	slices.Sort(suppressed)
	return selected, suppressed
}
