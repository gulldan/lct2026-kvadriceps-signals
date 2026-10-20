package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const liveSemanticMethod = "title-centroid-cosine-v2"
const liveSemanticSample = 12
const liveQualityDevelopment = "development_not_frozen"
const liveProfileTitlesAndAbstracts = "titles_and_abstracts"

// LiveSemantic — относительное сравнение, а не утверждение о новизне в мире.
type LiveSemantic struct {
	Method          string   `json:"method"`
	Model           string   `json:"model"`
	Revision        string   `json:"revision"`
	Compared        int      `json:"compared_topics"`
	PaperIDs        []string `json:"sample_paper_ids"`
	NearestID       string   `json:"nearest_id"`
	NearestName     string   `json:"nearest_name"`
	Similarity      float64  `json:"similarity"`
	Distance        float64  `json:"distance"`
	QuerySimilarity *float64 `json:"query_similarity"`
	// CorpusCohesion — шкала для QuerySimilarity: у широкого корпуса центр далёк от всего.
	CorpusCohesion   *float64           `json:"corpus_cohesion,omitempty"`
	ComparedIDs      []string           `json:"compared_topic_ids"`
	ComparedNames    []string           `json:"compared_topic_names"`
	NearestPaperIDs  []string           `json:"nearest_sample_paper_ids"`
	ProfileBasis     string             `json:"profile_basis"`
	MissingAbstracts int                `json:"missing_abstracts"`
	DistanceRange    *LiveSemanticRange `json:"sample_distance_range"`
	QualityStatus    string             `json:"quality_status"`
}

// LiveSemanticRange — разброс при исключении по одной работе, а не доверительный интервал.
type LiveSemanticRange struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
	Runs int     `json:"runs"`
}

type LiveEmbeddingBatch struct {
	Model, Revision string
	Vectors         [][]float64
}

type liveSemanticEncoder interface {
	Encode(context.Context, []string) (LiveEmbeddingBatch, error)
}

// SetSemanticEncoder подключает локальную модель до Start; запасного хеш-кодировщика нет.
func (s *TrendLiveService) SetSemanticEncoder(encoder liveSemanticEncoder) { s.semantic = encoder }

func liveTitleProfile(topic LiveTopic, papers []StudyPaper) ([]string, []string) {
	ids := map[string]bool{}
	for _, id := range topic.PaperIDs {
		ids[id] = true
	}
	for _, record := range liveChannelRecords(topic.Research) {
		ids[liveEvidenceID(record)] = true
	}
	selected := []StudyPaper{}
	for _, paper := range papers {
		if ids[paper.ID] && strings.TrimSpace(paper.Title) != "" {
			selected = append(selected, paper)
			delete(ids, paper.ID)
		}
	}
	slices.SortFunc(selected, func(a, b StudyPaper) int { return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.ID, b.ID)) })
	count := min(liveSemanticSample, len(selected))
	titles, sample := make([]string, 0, count), make([]string, 0, count)
	for i := range count {
		index := 0
		if count > 1 {
			index = i * (len(selected) - 1) / (count - 1)
		}
		paper := selected[index]
		title := []rune(strings.Join(strings.Fields(paper.Title), " "))
		titles = append(titles, string(title[:min(len(title), 256)]))
		sample = append(sample, paper.ID)
	}
	return titles, sample
}

func liveUnitVector(vector []float64) ([]float64, bool) {
	norm := 0.0
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, false
		}
		norm += value * value
	}
	if norm == 0 || math.IsInf(norm, 0) {
		return nil, false
	}
	result := slices.Clone(vector)
	for i := range result {
		result[i] /= math.Sqrt(norm)
	}
	return result, true
}

func liveSimilarity(a, b []float64) float64 {
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return max(-1, min(1, sum))
}

type liveSemanticProfile struct {
	topicIndex       int
	rows             []int
	paperIDs         []string
	basis            string
	missingAbstracts int
}

func (s *TrendLiveService) annotateSemanticMetadata(
	ctx context.Context, snapshot *LiveSnapshot, query string, metadata map[string]LivePaperMetadata,
) error {
	topics := slices.Concat(snapshot.Topics, snapshot.Discovered)
	for i := range topics {
		topics[i].Semantic = nil
	}
	defer func() {
		copy(snapshot.Topics, topics[:len(snapshot.Topics)])
		copy(snapshot.Discovered, topics[len(snapshot.Topics):])
	}()
	if s.semantic == nil {
		return nil
	}
	texts, inputs := liveSemanticInputs(topics, snapshot, metadata)
	if len(inputs) < 2 {
		return nil
	}
	if query != "" {
		texts = append(texts, query)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	batch, err := s.semantic.Encode(ctx, texts)
	if err != nil {
		return fmt.Errorf("semantic comparison model: %w", err)
	}
	if normalizeErr := liveNormalizeSemanticBatch(&batch, len(texts)); normalizeErr != nil {
		return normalizeErr
	}
	profiles, err := liveSemanticCentroids(inputs, batch.Vectors)
	if err != nil {
		return err
	}
	rows := make([][]int, len(inputs))
	for i, input := range inputs {
		rows[i] = input.rows
	}
	for i, input := range inputs {
		measurement := liveSemanticMeasurement(i, inputs, topics, profiles, batch)
		if measurement == nil {
			continue
		}
		measurement.DistanceRange = liveProfileSensitivity(i, rows, batch.Vectors, profiles)
		// Близость к запросу нужна и направлениям карты: по ней они становятся кандидатами ответа; вектор запроса
		// уже в пакете, отдельного обращения к модели не нужно.
		if query != "" {
			measurement.QuerySimilarity = new(liveSimilarity(profiles[i], batch.Vectors[len(texts)-1]))
		}
		if query != "" && input.topicIndex == 0 {
			measurement.CorpusCohesion = liveCorpusCohesion(input.rows, batch.Vectors, profiles[i])
		}
		topics[input.topicIndex].Semantic = measurement
	}
	return nil
}

func liveSemanticInputs(
	topics []LiveTopic, snapshot *LiveSnapshot, metadata map[string]LivePaperMetadata,
) ([]string, []liveSemanticProfile) {
	texts, inputs := []string{}, []liveSemanticProfile{}
	textRows := map[string]int{}
	for i, topic := range topics {
		titles, sample := liveTitleProfile(topic, snapshot.Papers)
		if len(titles) == 0 {
			continue
		}
		input := liveSemanticProfile{topicIndex: i, paperIDs: sample, basis: "titles"}
		for k, title := range titles {
			meta := metadata[sample[k]]
			if strings.TrimSpace(meta.Abstract) != "" && liveMetadataAtSnapshot(meta, snapshot) {
				title = liveSemanticText(title, meta.Abstract)
				input.basis = liveProfileTitlesAndAbstracts
			} else {
				input.missingAbstracts++
			}
			row, ok := textRows[title]
			if !ok {
				row = len(texts)
				textRows[title] = row
				texts = append(texts, title)
			}
			input.rows = append(input.rows, row)
		}
		inputs = append(inputs, input)
	}
	return texts, inputs
}

func liveNormalizeSemanticBatch(batch *LiveEmbeddingBatch, count int) error {
	if batch.Model == "" || batch.Revision == "" || len(batch.Vectors) != count {
		return errors.New("semantic comparison model returned incomplete vectors or provenance")
	}
	dimension := len(batch.Vectors[0])
	for i, vector := range batch.Vectors {
		unit, ok := liveUnitVector(vector)
		if !ok || len(unit) != dimension {
			return errors.New("semantic comparison model returned invalid vectors")
		}
		batch.Vectors[i] = unit
	}
	return nil
}

func liveSemanticCentroids(inputs []liveSemanticProfile, vectors [][]float64) ([][]float64, error) {
	profiles := make([][]float64, len(inputs))
	for i, input := range inputs {
		centroid := make([]float64, len(vectors[0]))
		for _, row := range input.rows {
			for d, value := range vectors[row] {
				centroid[d] += value
			}
		}
		unit, ok := liveUnitVector(centroid)
		if !ok {
			return nil, errors.New("semantic comparison profile is not a valid centroid")
		}
		profiles[i] = unit
	}
	return profiles, nil
}

func liveSemanticMeasurement(
	i int, inputs []liveSemanticProfile, topics []LiveTopic, profiles [][]float64, batch LiveEmbeddingBatch,
) *LiveSemantic {
	input := inputs[i]
	topic := topics[input.topicIndex]
	nearest, similarity := -1, -2.0
	for j, other := range inputs {
		if topic.ID == topics[other.topicIndex].ID {
			continue
		}
		candidate := liveSimilarity(profiles[i], profiles[j])
		if candidate > similarity {
			nearest, similarity = j, candidate
		}
	}
	if nearest < 0 {
		return nil
	}
	other := topics[inputs[nearest].topicIndex]
	measurement := &LiveSemantic{Method: liveSemanticMethod, Model: batch.Model, Revision: batch.Revision,
		Compared: len(inputs) - 1, PaperIDs: input.paperIDs, NearestID: other.ID, NearestName: other.Name,
		Similarity: similarity, Distance: 1 - max(0, similarity), ProfileBasis: input.basis,
		MissingAbstracts: input.missingAbstracts, NearestPaperIDs: inputs[nearest].paperIDs,
		QualityStatus: liveQualityDevelopment}
	if input.basis == liveProfileTitlesAndAbstracts {
		measurement.Method = "title-abstract-centroid-cosine-v2"
	}
	for _, otherInput := range inputs {
		other := topics[otherInput.topicIndex]
		if other.ID != topic.ID {
			measurement.ComparedIDs = append(measurement.ComparedIDs, other.ID)
			measurement.ComparedNames = append(measurement.ComparedNames, other.Name)
		}
	}
	return measurement
}

func liveSemanticText(title, abstract string) string {
	title = strings.Join(strings.Fields(title), " ")
	abstract = strings.Join(strings.Fields(abstract), " ")
	runes := []rune(title)
	title = string(runes[:min(len(runes), 256)])
	if abstract == "" {
		return title
	}
	runes = []rune(abstract)
	return title + "\n" + string(runes[:min(len(runes), 1400)])
}

func liveMetadataAtSnapshot(metadata LivePaperMetadata, snapshot *LiveSnapshot) bool {
	if len(snapshot.Periods) == 0 {
		return false
	}
	return liveBeforeCutoff(metadata.AvailableAt, snapshot.Periods[len(snapshot.Periods)-1].EndExclusive)
}

func liveProfileSensitivity(index int, rows [][]int, vectors, profiles [][]float64) *LiveSemanticRange {
	if len(rows[index]) < 2 {
		return nil
	}
	result := &LiveSemanticRange{Low: 1, High: 0}
	for omitted := range rows[index] {
		centroid := make([]float64, len(profiles[index]))
		for k, row := range rows[index] {
			if k == omitted {
				continue
			}
			for d, value := range vectors[row] {
				centroid[d] += value
			}
		}
		unit, ok := liveUnitVector(centroid)
		if !ok {
			continue
		}
		nearest := -1.0
		for j, profile := range profiles {
			if j != index {
				nearest = max(nearest, liveSimilarity(unit, profile))
			}
		}
		distance := 1 - max(0, nearest)
		result.Low, result.High = min(result.Low, distance), max(result.High, distance)
		result.Runs++
	}
	if result.Runs == 0 {
		return nil
	}
	return result
}

func liveCorpusCohesion(rows []int, vectors [][]float64, centroid []float64) *float64 {
	if len(rows) == 0 || len(centroid) == 0 {
		return nil
	}
	sum := 0.0
	for _, row := range rows {
		sum += liveSimilarity(vectors[row], centroid)
	}
	return new(sum / float64(len(rows)))
}
