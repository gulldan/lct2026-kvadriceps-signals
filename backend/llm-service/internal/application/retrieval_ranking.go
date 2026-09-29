package application

import (
	"github.com/example/llm-service/internal/domain"
	"sort"
)

// Недобор после потолка заполняется отсечёнными фрагментами, поэтому выдача не короче обычного топ-K.
func capPerDocument(ranked []domain.RetrievedChunk, k, maxPerDoc int) []domain.RetrievedChunk {
	if k > len(ranked) {
		k = len(ranked)
	}
	if maxPerDoc <= 0 {
		return ranked[:k]
	}
	perDoc := make(map[string]int, k)
	chosen := make([]domain.RetrievedChunk, 0, k)
	deferred := make([]domain.RetrievedChunk, 0, len(ranked))
	for _, c := range ranked {
		if len(chosen) >= k {
			break
		}
		if c.DocumentID != "" && perDoc[c.DocumentID] >= maxPerDoc {
			deferred = append(deferred, c)
			continue
		}
		perDoc[c.DocumentID]++
		chosen = append(chosen, c)
	}
	for _, c := range deferred {
		if len(chosen) >= k {
			break
		}
		chosen = append(chosen, c)
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Score > chosen[j].Score })
	return chosen
}

type weightedList struct {
	weight float64
	chunks []domain.RetrievedChunk
}

// Векторные попадания идут первыми: у них богаче payload, а поля происхождения берутся из первого вхождения.
func fuse(lists []weightedList) []domain.RetrievedChunk {
	type acc struct {
		chunk domain.RetrievedChunk
		score float64
	}
	merged := make(map[string]*acc)
	order := make([]string, 0)

	for _, list := range lists {
		for rank, chunk := range list.chunks {
			contribution := list.weight / float64(rrfK+rank)
			if a, ok := merged[chunk.ID]; ok {
				a.score += contribution
				mergeProvenance(&a.chunk, chunk)
				continue
			}
			merged[chunk.ID] = &acc{chunk: chunk, score: contribution}
			order = append(order, chunk.ID)
		}
	}

	out := make([]domain.RetrievedChunk, 0, len(order))
	for _, id := range order {
		a := merged[id]
		a.chunk.Score = a.score
		out = append(out, a.chunk)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}

func mergeProvenance(dst *domain.RetrievedChunk, src domain.RetrievedChunk) {
	switch {
	case dst.Origin == "":
		dst.Origin = src.Origin
	case src.Origin != "" && src.Origin != dst.Origin:
		dst.Origin = domain.OriginBoth
	}
	if dst.Text == "" {
		dst.Text = src.Text
	}
	if dst.DocumentID == "" {
		dst.DocumentID = src.DocumentID
	}
	if dst.Filename == "" {
		dst.Filename = src.Filename
	}
	if dst.ChunkIndex == 0 {
		dst.ChunkIndex = src.ChunkIndex
	}
	// Нулевой конец смещения значит «не задано» — тогда берём смещения из другой копии.
	if dst.CharEnd == 0 && src.CharEnd != 0 {
		dst.CharStart, dst.CharEnd = src.CharStart, src.CharEnd
	}
	if dst.PageEnd == 0 && src.PageEnd != 0 {
		dst.PageStart, dst.PageEnd = src.PageStart, src.PageEnd
	}
	if dst.SectionHeading == "" {
		dst.SectionHeading = src.SectionHeading
	}
}

// Ищем максимум, а не берём [0]: пул агентного режима не отсортирован.
func topRerankScore(chunks []domain.RetrievedChunk) (float64, bool) {
	top := 0.0
	ok := false
	for _, c := range chunks {
		if !ok || c.Score > top {
			top = c.Score
			ok = true
		}
	}
	return top, ok
}
