package application

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	liveExtractExpansionBatches    = 2
	liveExtractExpansionCandidates = 12
	liveExtractExpansionAbstract   = 1500
	liveExtractExpansionWait       = 2 * time.Minute
	liveExtractExcludedNames       = 2 * liveCandidatePool
	liveExtractNameRunes           = 160
)

// Один дополнительный проход получает общий срок, а не новый срок на каждую волну.
// Он предлагает только новые сущности: допуск и число мест остаются у строгого отбора.
func (s *TrendLiveService) expandExtraction(ctx context.Context, query string, documents []StudyPaper,
	metadata map[string]LivePaperMetadata, known []LiveTopic,
) []LiveExtractedTechnology {
	if s.extract == nil || query == "" || ctx.Err() != nil {
		return nil
	}
	ordered := liveExtractionOrder(documents)
	// Резерв определяется общим пакетом: настроенный внешний клиент мог отказать или прочитать лишь часть корпуса.
	examined := min(len(ordered), liveExtractDocuments)
	excluded := liveExtractionExclusions(known)
	pool := slices.Clone(ordered[examined:])
	limit := liveExtractExpansionBatches * liveExtractBatch
	if len(pool) < limit {
		pool = append(pool, liveExtractionRemainder(ordered[:examined], metadata, known)...)
	}
	pool = pool[:min(len(pool), limit)]
	batches := liveExtractBatches(pool, metadata, liveExtractExpansionAbstract)
	ctx, cancel := context.WithTimeout(ctx, liveExtractExpansionWait)
	defer cancel()
	// Добор запускается, когда волны уже заняли все места общего предела, а срок прохода общий: в очереди
	// за местом он истекал, и вместо новых механизмов проверялись словарные фразы. Два вызова сверх предела
	// на время одного прохода шлюз выдерживает.
	ctx = context.WithValue(ctx, liveWorkerSlotsKey{}, &liveWaveWorkers{calls: make(liveWorkerSlots, len(batches))})
	found := make([][]LiveExtractedTechnology, len(batches))
	s.eachConcurrently(ctx, len(batches), func(i int) {
		technologies, err := s.extract(ctx, CandidateExtractionRequest{
			Query: query, Documents: batches[i], Excluded: excluded, Limit: liveExtractExpansionCandidates,
		})
		if err != nil {
			slog.Warn("radar additional extraction unavailable", "batch", i, "error", err)
			return
		}
		technologies = liveNewExtractions(technologies, excluded)
		found[i] = technologies[:min(len(technologies), liveExtractExpansionCandidates)]
	})
	return slices.Concat(found...)
}

// Слоты делятся между источниками и годами: свежий поток одной площадки не вытесняет остальные.
// Копии одного первоисточника не расходуют бюджет повторно; дописываемые паспорта не меняют порядок пакета.
func liveExtractionOrder(documents []StudyPaper) []StudyPaper {
	groups := map[string][]StudyPaper{}
	seen := map[string]bool{}
	for _, paper := range liveUniquePapers(documents) {
		identities := []string{"id:" + paper.ID}
		if paper.CanonicalID != "" {
			identities = append(identities, "canonical:"+paper.CanonicalID)
		}
		if address := liveEvidenceURL(paper.URL); address != "" {
			identities = append(identities, "url:"+address)
		}
		if slices.ContainsFunc(identities, func(key string) bool { return seen[key] }) {
			continue
		}
		for _, identity := range identities {
			seen[identity] = true
		}
		key := liveSourceOf(paper.ID) + ":" + paper.Date[:min(4, len(paper.Date))]
		groups[key] = append(groups[key], paper)
	}
	keys := slices.Sorted(maps.Keys(groups))
	for _, group := range groups {
		slices.SortFunc(group, func(a, b StudyPaper) int {
			return cmp.Or(-cmp.Compare(a.Date, b.Date), cmp.Compare(a.ID, b.ID))
		})
	}
	ordered := make([]StudyPaper, 0, len(documents))
	for row := 0; ; row++ {
		before := len(ordered)
		for _, key := range keys {
			if row < len(groups[key]) {
				ordered = append(ordered, groups[key][row])
			}
		}
		if len(ordered) == before {
			return ordered
		}
	}
}

// Сначала перечитываются насыщенные источники: первые восемь имён могли не вместить обзор целиком.
// Более длинная аннотация открывает контекст, которого не было в первом проходе.
func liveExtractionRemainder(documents []StudyPaper, metadata map[string]LivePaperMetadata,
	known []LiveTopic,
) []StudyPaper {
	mentions := map[string]int{}
	for _, topic := range known {
		for _, id := range slices.Concat(topic.PaperIDs, topic.EvidenceIDs) {
			mentions[id]++
		}
	}
	pool := slices.DeleteFunc(slices.Clone(documents), func(paper StudyPaper) bool {
		return len(known) == 0 && utf8.RuneCountInString(metadata[paper.ID].Abstract) <= liveExtractAbstract
	})
	slices.SortStableFunc(pool, func(a, b StudyPaper) int {
		return cmp.Or(-cmp.Compare(mentions[a.ID], mentions[b.ID]),
			-cmp.Compare(utf8.RuneCountInString(metadata[a.ID].Abstract), utf8.RuneCountInString(metadata[b.ID].Abstract)))
	})
	return pool
}

func liveExtractionExclusions(known []LiveTopic) []string {
	names := []string{}
	for _, topic := range known {
		labels := []string{topic.Name, topic.Label}
		if topic.Qualification != nil {
			labels = append(labels, topic.Qualification.Label)
		}
		for _, label := range labels {
			label = liveClip(strings.Join(strings.Fields(label), " "), liveExtractNameRunes)
			if label != "" && !slices.Contains(names, label) {
				names = append(names, label)
			}
		}
	}
	return names[:min(len(names), liveExtractExcludedNames)]
}

func liveNewExtractions(found []LiveExtractedTechnology, excluded []string) []LiveExtractedTechnology {
	known := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		if key := liveLabelKey(name); key != "" {
			known[key] = true
		}
	}
	return slices.DeleteFunc(found, func(technology LiveExtractedTechnology) bool {
		return known[liveLabelKey(technology.Name)] || known[liveLabelKey(technology.Label)]
	})
}
