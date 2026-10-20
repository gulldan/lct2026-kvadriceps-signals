package application

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"
)

// livePrepareAnswerMetadata сохраняет паспорт рыночного материала: новость не должна стать научной статьёй.
func livePrepareAnswerMetadata(candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata) {
	if metadata == nil {
		return
	}
	for _, candidate := range candidates {
		channels := []*LiveChannel{candidate.Patents, candidate.PatentsCN, candidate.Registry, candidate.Web, candidate.Research}
		for _, channel := range channels {
			for _, record := range liveChannelRecords(channel) {
				id := liveRecordPrefix + cmp.Or(record.ID, record.URL)
				meta := metadata[id]
				meta.SourceURL = cmp.Or(record.OriginalURL, record.URL, record.CanonicalURL)
				meta.Language, meta.LanguageBasis = record.Language, record.LanguageBasis
				meta.PublisherType, meta.Trust, meta.TrustBasis = record.PublisherType, record.Trust, record.TrustBasis
				meta.Genre = cmp.Or(record.Genre, meta.Genre)
				meta.DOI = cmp.Or(record.DOI, meta.DOI)
				if provider := liveJournalProvider(record.Source); provider != "" {
					// Идентификатор по цифровому адресу публикации не содержит имени каталога.
					meta.Category = provider
					meta.ObservedAt = cmp.Or(record.ObservedAt, meta.ObservedAt)
					meta.AvailableAt = cmp.Or(record.PublishedAt, record.AvailableAt, record.Date, meta.AvailableAt)
				}
				// Фрагмент рыночной записи встаёт на место аннотации: по нему карточка пишет кейс, а пересказ — резюме.
				meta.Abstract = cmp.Or(meta.Abstract, record.Excerpt)
				metadata[id] = meta
			}
		}
	}
	for _, document := range documents {
		meta := metadata[document.ID]
		meta.SourceURL = cmp.Or(meta.SourceURL, document.URL)
		if meta.PublisherType == "" || meta.Trust == "" {
			passport, _ := liveApplyGenre(ClassifySource(meta.SourceURL), meta.SourceURL, document.Title)
			meta.PublisherType, meta.Trust, meta.TrustBasis = passport.Publisher, passport.Trust, passport.TrustBasis
		}
		meta.Genre = cmp.Or(meta.Genre, ClassifyGenre(meta.SourceURL, document.Title).Genre)
		meta.Language, meta.LanguageBasis = livePaperLanguage(document, meta)
		metadata[document.ID] = meta
	}
}

func (s *TrendLiveService) cardJobs(ctx context.Context, query string, candidates []LiveTopic, documents []StudyPaper,
	metadata map[string]LivePaperMetadata,
) []func() {
	if s.card == nil {
		return nil
	}
	byID := make(map[string]StudyPaper, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	jobs := make([]func(), len(candidates))
	for i := range candidates {
		jobs[i] = func() { s.writeCard(ctx, query, &candidates[i], byID, metadata) }
	}
	return jobs
}

// Отказ оставляет поле пустым и названным в Missing, а не заполняет заглушкой.
func (s *TrendLiveService) writeCard(ctx context.Context, query string, candidate *LiveTopic,
	byID map[string]StudyPaper, metadata map[string]LivePaperMetadata,
) {
	defer liveKeepCitedMarket(candidate)
	if !liveVerifiedMechanism(*candidate) {
		candidate.Limits = append(candidate.Limits, liveLimitOf(liveLimitCardOff,
			"карточка не составлена: механизм технологии не прошёл проверку сущности", nil))
		return
	}
	works, used := liveCardInput(*candidate, byID, metadata)
	if len(works) == 0 {
		return
	}
	contexts := liveCardContexts(ctx, works, used, byID, metadata)
	identity := LiveCardIdentity{Label: candidate.Qualification.Label, Mechanism: candidate.Qualification.Mechanism}
	card, err := s.card(ctx, query, identity, works)
	if err != nil {
		candidate.Limits = append(candidate.Limits, liveLimitFail(liveLimitCardOff, "card", err))
		return
	}
	card.SourceContexts = contexts
	card.Basis += liveCardContextBasis(contexts)
	liveRemapClaimSources(card.Evidence, used)
	card.ExampleID = ""
	if example := liveClaim(card.Evidence, liveClaimExample); example != nil {
		card.ExampleID = example.SourceID
		card.Basis += liveLoweredExample(*candidate, metadata, example.SourceID)
	}
	if card.market != nil {
		liveRemapClaimSources(card.market.Evidence, used)
		candidate.Market = card.market
	}
	if card.adoption != nil {
		liveRemapClaimSources(card.adoption.Evidence, used)
		candidate.Adoption = card.adoption
	}
	candidate.Card = &card
}

// liveCardMarket — сколько из liveCardWorks мест в карточке отдано рыночным записям: научных работ у
// кандидата обычно больше восьми, и без квоты кейс из пилота или раунда не появился бы.
const liveCardMarket = 3

// Позиция источника в запросе используется для обратной привязки цитат.
func liveCardInput(topic LiveTopic, documents map[string]StudyPaper,
	metadata map[string]LivePaperMetadata,
) (works, ids []string) {
	known := func(list []string) []string {
		return slices.DeleteFunc(slices.Clone(list), func(id string) bool {
			_, ok := documents[id]
			return !ok || liveInvestorContext(metadata[id].Genre) || liveInvestorSource(topic, id)
		})
	}
	papers, records := known(topic.PaperIDs), known(topic.EvidenceIDs)
	papers = papers[:min(len(papers), liveCardWorks-min(len(records), liveCardMarket))]
	// Записи пилотов и контрактов идут первыми: кейс ищется в начале списка.
	events := liveEventRecordIDs(topic)
	first := slices.DeleteFunc(slices.Clone(records), func(id string) bool { return !events[id] })
	records = slices.DeleteFunc(records, func(id string) bool { return events[id] })
	for _, id := range slices.Concat(first, papers, records) {
		if slices.Contains(ids, id) {
			continue
		}
		works = append(works, liveWorkLine(documents[id], metadata[id]))
		ids = append(ids, id)
		if len(works) == liveCardWorks {
			break
		}
	}
	return works, ids
}

// Пресс-релиз подтверждает, что о пилоте объявили, а не что он состоялся.
func liveLoweredExample(topic LiveTopic, metadata map[string]LivePaperMetadata, exampleID string) string {
	example := metadata[exampleID]
	if example.Trust != TrustLowered {
		return ""
	}
	host := passportHost(example.SourceURL)
	independent := 0
	for _, id := range slices.Concat(topic.PaperIDs, topic.EvidenceIDs) {
		meta := metadata[id]
		if id != exampleID && liveTrustOrder(meta.Trust) > liveTrustOrder(TrustLowered) &&
			passportHost(meta.SourceURL) != host {
			independent++
		}
	}
	note := "; кейс-пример взят из источника пониженного доверия (" + example.TrustBasis +
		"): это первичный индикатор, а не подтверждение заявленного"
	if independent == 0 {
		return note + "; независимых источников обычного доверия среди найденных нет"
	}
	return note + fmt.Sprintf("; саму технологию подтверждают независимые источники обычного доверия: %d", independent)
}

func liveRemapClaimSources(evidence []LiveClaimEvidence, ids []string) {
	for i := range evidence {
		position, err := strconv.Atoi(evidence[i].SourceID)
		if err != nil || position < 1 || position > len(ids) {
			evidence[i].SourceID, evidence[i].Verified = "", false
			continue
		}
		evidence[i].SourceID = ids[position-1]
	}
}

// Список снимается до начала работы: карточка может убрать непроверенную рыночную запись.
func (s *TrendLiveService) digestJobs(ctx context.Context, candidates []LiveTopic, papers []StudyPaper,
	metadata map[string]LivePaperMetadata,
) (jobs []func(), apply func()) {
	wanted := liveDigestWanted(candidates, metadata)
	if metadata == nil || len(wanted) == 0 {
		return nil, func() {}
	}
	title := make(map[string]string, len(papers))
	for _, paper := range papers {
		title[paper.ID] = paper.Title
	}
	// Каждый работник пишет в свою ячейку, общая карта меняется после всех обращений: карточки читают её без блокировок.
	updates := make([]LivePaperMetadata, len(wanted))
	jobs = make([]func(), len(wanted))
	for i, id := range wanted {
		updates[i] = metadata[id]
		jobs[i] = func() {
			meta := updates[i]
			if s.digest == nil || title[id] == "" {
				return
			}
			digest, model, err := s.digest(ctx, title[id], meta.Abstract)
			if err != nil || digest == "" || model == "" {
				return
			}
			meta.Digest, meta.DigestModel = liveDigestScope(digest, meta.Abstract), model
			updates[i] = meta
		}
	}
	return jobs, func() {
		for i, id := range wanted {
			metadata[id] = updates[i]
		}
		liveDigestLimits(candidates, metadata)
	}
}

func liveDigestLimits(candidates []LiveTopic, metadata map[string]LivePaperMetadata) {
	for i := range candidates {
		candidate := &candidates[i]
		for _, id := range slices.Concat(candidate.PaperIDs, candidate.EvidenceIDs) {
			meta := metadata[id]
			if meta.Language != "ru" && (meta.Digest == "" || meta.DigestModel == "") {
				candidate.Limits = append(candidate.Limits,
					liveLimitOf("digest_unavailable", fmt.Sprintf("русская интерпретация источника %s недоступна", id), nil))
			}
		}
	}
}

func liveDigestScope(digest, abstract string) string {
	if abstract == "" {
		return "Пересказ только заголовка: " + digest
	}
	if utf8.RuneCountInString(abstract) > liveCardAbstract {
		return "Пересказ доступного фрагмента аннотации: " + digest
	}
	return digest
}

func liveDigestWanted(candidates []LiveTopic, metadata map[string]LivePaperMetadata) []string {
	wanted := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		for _, id := range slices.Concat(candidate.PaperIDs, candidate.EvidenceIDs) {
			meta := metadata[id]
			if seen[id] || (meta.Digest != "" && meta.DigestModel != "") || meta.Language == "ru" {
				continue
			}
			seen[id] = true
			wanted = append(wanted, id)
		}
	}
	return wanted
}
