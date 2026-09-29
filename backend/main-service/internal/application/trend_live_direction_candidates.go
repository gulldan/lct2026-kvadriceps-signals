package application

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Извлечение из корпуса запроса находит методы отдельных статей: у каждого одна группа авторов и ни одного
// повторения. Направления карты — технологии уровня тренда: их ряд, группы и стадия сняты по полному окну arXiv,
// а не по выборке запроса, поэтому релевантные запросу направления идут в очередь проверки первыми.

const (
	liveDirectionPrefix = "direction:"
	// liveDirectionMinShared: меньше пяти общих работ с корпусом запроса — случайное пересечение, а не технология
	// этой области. На «технологиях в ИИ» порог оставляет «Сжатие KV-кэша» (6 работ) и отсекает Model Context
	// Protocol (4) и спекулятивное декодирование (0): их корпус запроса почти не содержит.
	liveDirectionMinShared = 5
	// liveDirectionLimit: направления открывают первую волну и не должны вытеснить из неё извлечённые технологии.
	liveDirectionLimit = 10
)

// liveDirectionCandidates отказывает по тем же правилам ТЗ, что и остальным кандидатам, но уже по ряду направления:
// на ряде выборки запроса давняя история и спад неизмеримы.
func liveDirectionCandidates(umbrella LiveTopic, papers []StudyPaper, directions []LiveTopic, periods []LivePeriod,
) ([]LiveTopic, []LiveRejection) {
	corpus := make(map[string]StudyPaper, len(papers))
	for _, paper := range papers {
		corpus[paper.ID] = paper
	}
	bar := liveMarketBarOf(umbrella)
	tier := liveEvidenceTiers()[len(liveEvidenceTiers())-1]
	type scored struct {
		topic  LiveTopic
		shared int
	}
	kept := []scored{}
	rejected := []LiveRejection{}
	for _, direction := range directions {
		shared := []StudyPaper{}
		for _, id := range direction.PaperIDs {
			if paper, ok := corpus[id]; ok {
				shared = append(shared, paper)
			}
		}
		// Направление, которому принадлежит больше половины корпуса, — сам запрос, а не технология внутри него.
		if len(shared) < liveDirectionMinShared || 2*len(shared) > len(papers) {
			continue
		}
		topic := liveDirectionCandidate(umbrella, direction, shared, periods, bar)
		// Та же мера и тот же порог, что у корпуса запроса: общие работы бывают лишь применением метода запроса
		// в чужой области («ИИ в термоядерном синтезе»), и тогда ряд направления — не ряд этой технологии.
		similarity, measured := liveQueryRelevance(direction)
		if percent := int(similarity*100 + 0.5); measured && percent < liveRelevanceFloor {
			rejected = append(rejected, liveRejection(topic, liveRejectOffTopic, fmt.Sprintf(
				"направление карты не о том, что спросили: близость к запросу %d %% при пороге %d %%",
				percent, liveRelevanceFloor)))
			continue
		}
		if code, reason := liveRejectReason(topic, tier, bar); code != "" {
			rejected = append(rejected, liveRejection(topic, code, reason))
			continue
		}
		kept = append(kept, scored{topic, len(shared)})
	}
	// Сначала зарождающиеся по ряду, затем слабые по карте Юна, затем ближайшие к запросу по числу общих работ.
	slices.SortStableFunc(kept, func(a, b scored) int {
		return cmp.Or(cmp.Compare(liveTopUpLate(a.topic), liveTopUpLate(b.topic)),
			cmp.Compare(liveEmergenceRank(a.topic.Emergence), liveEmergenceRank(b.topic.Emergence)),
			-cmp.Compare(a.shared, b.shared))
	})
	candidates := make([]LiveTopic, 0, min(len(kept), liveDirectionLimit))
	for i, row := range kept {
		if i < liveDirectionLimit {
			candidates = append(candidates, row.topic)
			continue
		}
		rejected = append(rejected, liveRejection(row.topic, liveRejectBudget, fmt.Sprintf(
			"не проверено по содержанию: в одном поиске проверяется не более %d направлений карты",
			liveDirectionLimit)))
	}
	return candidates, rejected
}

// Из корпуса запроса берутся только свежие общие работы — их читает проверка сущности — и записи каналов,
// называющие технологию; всё измеренное — от направления.
func liveDirectionCandidate(umbrella, direction LiveTopic, shared []StudyPaper, periods []LivePeriod,
	bar liveMarketBar,
) LiveTopic {
	slices.SortStableFunc(shared, func(a, b StudyPaper) int { return cmp.Compare(b.Date, a.Date) })
	shared = shared[:min(len(shared), liveQualificationDocuments)]
	term := liveDirectionTerm(direction)
	topic := liveCandidateTopic(umbrella, liveCandidate{gram: term, surface: term, papers: shared}, periods, bar, nil)
	topic.ID, topic.Name, topic.Query = liveDirectionPrefix+direction.ID, direction.Name, direction.Query
	topic.Label = cmp.Or(direction.Label, direction.Name)
	// Оговорка о выборке запроса к ряду направления не относится: он снят по полному окну.
	topic.Limits = nil
	topic.EvidenceLevel = liveEvidenceThin
	liveDirectionMeasure(&topic, direction)
	topic.Market = liveMarketMaturity(topic, bar).field()
	topic.Confidence = new(liveConfidence(topic, bar))
	return topic
}

// liveDirectionMeasure переносит измерения направления, оставляя кандидату его собственные источники.
func liveDirectionMeasure(topic *LiveTopic, direction LiveTopic) {
	own := topic.StudyTopic
	topic.StudyTopic = direction.StudyTopic
	topic.Series = slices.Clone(direction.Series)
	topic.ID, topic.PaperIDs, topic.EvidenceIDs = own.ID, own.PaperIDs, own.EvidenceIDs
	topic.Screening, topic.Reviewed = own.Screening, own.Reviewed
	topic.External, topic.EarliestExternal = own.External, own.EarliestExternal
	topic.Emergence, topic.Comparison = direction.Emergence, direction.Comparison
	topic.PapersCount = direction.PapersCount
	topic.DocumentsCount = topic.PapersCount + topic.RecordsCount
}

func liveFromDirection(topic LiveTopic) bool {
	return strings.HasPrefix(topic.ID, liveDirectionPrefix)
}

// Первый термин направления — родовое имя технологии: по нему модель первого этапа обучена на эталоне.
func liveDirectionTerm(topic LiveTopic) string {
	if len(topic.Query.Terms) > 0 {
		return strings.ToLower(topic.Query.Terms[0])
	}
	return strings.ToLower(topic.Name)
}

// Извлечённая технология и направление карты названы по существу; n-грамма словаря — обрывок фразы.
func liveNamedCandidate(topic LiveTopic) bool {
	return strings.HasPrefix(topic.ID, liveExtractPrefix) || liveFromDirection(topic)
}
