package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
)

// liveMarketRecords мал: каждая запись требует пересказа, и его отказ снял бы технологию не по её вине.
const liveMarketRecords = 3

// liveMarketParallel: на каждое имя сборщик спрашивает и Bing News, а больше шести запросов разом тот ограничивает.
const liveMarketParallel = 6

// marketEvidence спрашивает по каждому кандидату: общая фраза одного вытеснила бы записи другого из предела ответа.
func (s *TrendLiveService) marketEvidence(ctx context.Context, umbrella *LiveTopic, candidates []LiveTopic,
	periods []LivePeriod,
) {
	base := strings.TrimRight(os.Getenv("WEB_COLLECTOR_URL"), "/")
	if base == "" {
		return
	}
	src := s.newSources()
	failures := make([]error, len(candidates))
	slot := make(chan struct{}, liveMarketParallel)
	var wg sync.WaitGroup
	for i := range candidates {
		term := liveMarketTerm(candidates[i])
		if term == "" {
			continue
		}
		wg.Go(func() {
			slot <- struct{}{}
			defer func() { <-slot }()
			if ctx.Err() != nil {
				return
			}
			body, err := json.Marshal(struct {
				Terms   []string     `json:"terms"`
				Periods []LivePeriod `json:"periods"`
				Titles  bool         `json:"titles"`
			}{[]string{term}, periods, true})
			if err != nil {
				failures[i] = fmt.Errorf("encode market query: %w", err)
				return
			}
			channel, err := src.queryChannel(ctx, base, body, len(periods))
			if err != nil {
				failures[i] = err
				return
			}
			liveAttachMarket(&candidates[i], channel, term)
		})
	}
	wg.Wait()
	if at := slices.IndexFunc(failures, func(err error) bool { return err != nil }); at >= 0 {
		umbrella.Limits = append(umbrella.Limits, liveLimitFail(liveLimitChannelOff, "WEB_COLLECTOR_URL", failures[at]))
	}
}

// liveMarketTerm берёт английскую фразу: русское название модель пишет своими словами, в лентах его нет.
func liveMarketTerm(candidate LiveTopic) string {
	if (candidate.Qualification == nil || !candidate.Qualification.Verified) && candidate.Shortfall == nil {
		return ""
	}
	term := cmp.Or(candidate.Query.HN, strings.ToLower(candidate.Name))
	if len(liveWords(term)) < 2 || len(term) > 200 {
		return ""
	}
	return term
}

func liveAttachMarket(candidate *LiveTopic, channel *LiveChannel, term string) {
	have := map[string]bool{}
	for _, id := range candidate.EvidenceIDs {
		have[id] = true
	}
	excerpts := map[string]string{}
	for _, record := range channel.Records {
		excerpts[liveEvidenceID(record)] = record.Excerpt
	}
	// Общий сбор отдаёт записи без фрагмента: унаследованной записи карточка видела один заголовок, а копию
	// с фрагментом из этого запроса отбрасывала как повтор.
	if candidate.Web != nil {
		web := *candidate.Web
		web.Records = slices.Clone(web.Records)
		for i, record := range web.Records {
			have[liveEvidenceID(record)] = true
			web.Records[i].Excerpt = cmp.Or(record.Excerpt, excerpts[liveEvidenceID(record)])
		}
		candidate.Web = &web
	}
	picked := []LiveSourceRecord{}
	for _, record := range channel.Records {
		if !have[liveEvidenceID(record)] && liveMarketPublisher(record.PublisherType) {
			have[liveEvidenceID(record)] = true
			picked = append(picked, record)
		}
	}
	if len(picked) == 0 {
		return
	}
	slices.SortStableFunc(picked, func(a, b LiveSourceRecord) int {
		return cmp.Or(-cmp.Compare(liveTrustOrder(a.Trust), liveTrustOrder(b.Trust)), -cmp.Compare(a.Date, b.Date))
	})
	picked = picked[:min(len(picked), liveMarketRecords)]
	web := LiveChannel{SourceSet: channel.SourceSet, Source: channel.Source, ObservedAt: channel.ObservedAt,
		Partial: channel.Partial, Counts: []*int{}}
	if candidate.Web != nil {
		web = *candidate.Web
	}
	web.Records = append(slices.Clone(web.Records), picked...)
	web.Total = len(web.Records)
	web.Limits = append(slices.Clone(web.Limits), fmt.Sprintf(
		"рыночные записи найдены отдельным запросом к сборщику лент: «%s» стоит в заголовке материала; "+
			"остаются те, на которые опирается карточка", term))
	candidate.Web = &web
	ids := make([]string, len(picked))
	for i, record := range picked {
		ids[i] = liveEvidenceID(record)
	}
	// Записям карточка отдаёт три места из восьми по порядку списка: в конце рыночные уступали их патентам.
	candidate.EvidenceIDs = slices.Concat(ids, candidate.EvidenceIDs)
	candidate.marketIDs = append(candidate.marketIDs, ids...)
	candidate.RecordsCount += len(picked)
	candidate.DocumentsCount += len(picked)
}

// liveKeepCitedMarket оставляет только процитированные записи: общая фраза («real time») в заголовке часто о другом.
func liveKeepCitedMarket(topic *LiveTopic) {
	if len(topic.marketIDs) == 0 {
		return
	}
	cited := map[string]bool{}
	if topic.Card != nil {
		for _, evidence := range topic.Card.Evidence {
			cited[evidence.SourceID] = cited[evidence.SourceID] || evidence.Verified
		}
	}
	dropped := map[string]bool{}
	for _, id := range topic.marketIDs {
		if !cited[id] {
			dropped[id] = true
		}
	}
	topic.marketIDs = nil
	if len(dropped) == 0 {
		return
	}
	topic.EvidenceIDs = slices.DeleteFunc(slices.Clone(topic.EvidenceIDs), func(id string) bool { return dropped[id] })
	topic.RecordsCount -= len(dropped)
	topic.DocumentsCount -= len(dropped)
	web := *topic.Web
	web.Records = slices.DeleteFunc(slices.Clone(web.Records), func(record LiveSourceRecord) bool {
		return dropped[liveEvidenceID(record)]
	})
	web.Total = len(web.Records)
	topic.Web = &web
	if web.Total == 0 {
		topic.Web = nil
	}
}

func liveEvidenceID(record LiveSourceRecord) string {
	return liveRecordPrefix + cmp.Or(record.ID, record.URL)
}

// liveMarketPublisher нужен, потому что в лентах веб-сборщика рядом стоят PLOS, Frontiers, Nature и новости вузов.
func liveMarketPublisher(publisher string) bool {
	switch publisher {
	case PublisherJournal, PublisherPreprint, PublisherUniversity, PublisherResearch:
		return false
	}
	return true
}

// liveWithCandidateRecords нужен, иначе записи, найденные после разбора, пропадут из кейсов и списка источников.
func liveWithCandidateRecords(candidates []LiveTopic, documents []StudyPaper) []StudyPaper {
	have := make(map[string]bool, len(documents))
	for _, document := range documents {
		have[document.ID] = true
	}
	for _, candidate := range candidates {
		for _, channel := range []*LiveChannel{candidate.Patents, candidate.PatentsCN, candidate.Registry, candidate.Web} {
			for _, record := range liveChannelRecords(channel) {
				document, ok := liveRecordDocument(record)
				if ok && !have[document.ID] {
					have[document.ID] = true
					documents = append(documents, document)
				}
			}
		}
	}
	return documents
}
