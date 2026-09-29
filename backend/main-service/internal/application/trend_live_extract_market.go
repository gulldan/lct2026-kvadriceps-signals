package application

import (
	"cmp"
	"context"
	"slices"
	"strings"
)

// Эталон ТЗ почти целиком из отраслевых медиа и сайтов компаний, а они называют технологию классом продукта,
// а не методом статьи: рыночные записи запроса читаются отдельным проходом со своим промптом.

const (
	// liveExtractMarketBatches: пакеты идут одновременно с научными и к сроку шага не прибавляют; по восемь имён
	// на пакет — не больше шестнадцати рыночных кандидатов, очередь проверки ими не переполняется.
	liveExtractMarketBatches = 2
	// liveExtractMarketPublisher — восьмая часть пакетов: одна лента не займёт весь проход, а технологии
	// нужны подтверждения разных изданий.
	liveExtractMarketPublisher = 6
)

// withMarketExtraction запускает рыночный проход рядом с научным: шаг ждёт дольшего из двух, а не суммы.
func (s *TrendLiveService) withMarketExtraction(ctx context.Context, query string, umbrella *LiveTopic,
	metadata map[string]LivePaperMetadata, general func() []LiveExtractedTechnology,
) func() []LiveExtractedTechnology {
	documents := liveMarketDocuments(*umbrella)
	// Без метаданных фрагменту некуда перейти, и он остаётся в записи.
	if metadata != nil {
		livePrepareAnswerMetadata([]LiveTopic{*umbrella}, documents, metadata)
		liveDropExcerpts(umbrella)
	}
	if s.extract == nil || query == "" || len(documents) == 0 {
		return general
	}
	batches := liveExtractBatches(liveMarketPool(*umbrella, documents, metadata), metadata, liveExtractAbstract)
	market := s.runExtraction(ctx, query, batches, true)
	return func() []LiveExtractedTechnology { return slices.Concat(general(), market()) }
}

// Сборщик уже отобрал записи лент по фразам плана в заголовке или тексте; научные издатели в лентах — не рынок.
func liveMarketDocuments(topic LiveTopic) []StudyPaper {
	documents := []StudyPaper{}
	for _, record := range liveChannelRecords(topic.Web) {
		if document, ok := liveRecordDocument(record); ok && liveMarketPublisher(record.PublisherType) {
			documents = append(documents, document)
		}
	}
	return liveUniquePapers(documents)
}

// Первыми — записи о предмете запроса с текстом и доверенным издателем, затем свежие.
func liveMarketPool(topic LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata) []StudyPaper {
	terms := liveQueryTerms(topic)
	closeness := func(document StudyPaper) int {
		score := 0
		if liveAboutQuery(document.Title, terms) {
			score += 2
		}
		if liveAboutQuery(metadata[document.ID].Abstract, terms) {
			score++
		}
		return score
	}
	ranked := slices.Clone(documents)
	slices.SortStableFunc(ranked, func(a, b StudyPaper) int {
		return cmp.Or(-cmp.Compare(closeness(a), closeness(b)),
			-cmp.Compare(liveTrustOrder(metadata[a.ID].Trust), liveTrustOrder(metadata[b.ID].Trust)),
			-cmp.Compare(a.Date, b.Date))
	})
	limit := liveExtractMarketBatches * liveExtractBatch
	pool := make([]StudyPaper, 0, limit)
	taken := map[string]int{}
	for _, document := range ranked {
		publisher := cmp.Or(passportHost(document.URL), strings.Join(document.Authors, ", "))
		if taken[publisher] < liveExtractMarketPublisher && len(pool) < limit {
			taken[publisher]++
			pool = append(pool, document)
		}
	}
	return pool
}

// Заголовочный отбор каналов пропускает малую часть лент, а рыночному кандидату нужны все его записи.
func liveWithMarketDocuments(documents []StudyPaper, topic LiveTopic) []StudyPaper {
	have := make(map[string]bool, len(documents))
	for _, document := range documents {
		have[document.ID] = true
	}
	for _, document := range liveMarketDocuments(topic) {
		if !have[document.ID] {
			have[document.ID] = true
			documents = append(documents, document)
		}
	}
	return documents
}

// Фрагменты уже стали аннотациями в метаданных: в сохранённом ответе они повторили бы сотни килобайт лент.
func liveDropExcerpts(topic *LiveTopic) {
	if topic.Web == nil {
		return
	}
	web := *topic.Web
	web.Records = slices.Clone(web.Records)
	for i := range web.Records {
		web.Records[i].Excerpt = ""
	}
	topic.Web = &web
}

// Кандидаты из записей чередуются с научными: в конце очереди их отрезал бы срок проверки, а подряд они
// вытеснили бы науку.
func liveInterleaveRecords(candidates []LiveTopic) []LiveTopic {
	var science, records []LiveTopic
	for _, candidate := range candidates {
		if slices.ContainsFunc(candidate.qualificationAnchors, func(id string) bool {
			return strings.HasPrefix(id, liveRecordPrefix)
		}) {
			records = append(records, candidate)
		} else {
			science = append(science, candidate)
		}
	}
	ordered := make([]LiveTopic, 0, len(candidates))
	for i := range max(len(science), len(records)) {
		if i < len(science) {
			ordered = append(ordered, science[i])
		}
		if i < len(records) {
			ordered = append(ordered, records[i])
		}
	}
	return ordered
}
