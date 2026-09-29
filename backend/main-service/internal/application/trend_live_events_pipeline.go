package application

import (
	"cmp"
	"context"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// liveEventWait: открытые API отвечают за одну-три секунды, медленный источник дожидается в фоне.
	liveEventWait = 10 * time.Second
	// liveEventTTL: за сутки события по имени не меняются настолько, чтобы снова тратить лимиты источника.
	liveEventTTL = 24 * time.Hour
	// liveEventFetchTimeout: дольше ответ уже не попадёт ни в поиск, ни в пересохранение.
	liveEventFetchTimeout = 90 * time.Second
	liveEventCacheSize    = 4096
	// liveEventKept: больше — лишний вес ответа, а стадию и кейс решают первые по стадии.
	liveEventKept = 20
	// liveEventCardRecords: каждая зарубежная запись карточки требует пересказа в той же волне.
	liveEventCardRecords = 2
)

// liveEventEntry: поля пишутся до закрытия done и читаются только после него.
type liveEventEntry struct {
	done   chan struct{}
	events []LiveAdoptionEvent
	err    error
	at     time.Time
}

func (e *liveEventEntry) finished() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

// liveEventCache держит предел запросов на каждый источник: медленный не должен занимать места быстрого.
type liveEventCache struct {
	mu    sync.Mutex
	known map[string]*liveEventEntry
	slots map[string]chan struct{}
}

func newLiveEventCache() *liveEventCache {
	return &liveEventCache{known: map[string]*liveEventEntry{}, slots: map[string]chan struct{}{}}
}

func liveEventKey(source, term string) string { return source + "\n" + term }

// fetch не кеширует отказ: следующий поиск спросит снова.
func (c *liveEventCache) fetch(ctx context.Context, client *http.Client, source LiveEventSource,
	query LiveEventQuery, now func() time.Time,
) {
	key := liveEventKey(source.Name, query.Term)
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.known[key]; ok && (!entry.finished() || entry.err == nil && now().Sub(entry.at) < liveEventTTL) {
		return
	}
	// ponytail: чистка перебором всего кэша; при сотне тысяч имён — очередь по сроку.
	if len(c.known) > liveEventCacheSize {
		for old, entry := range c.known {
			if entry.finished() && now().Sub(entry.at) >= liveEventTTL {
				delete(c.known, old)
			}
		}
	}
	slot, ok := c.slots[source.Name]
	if !ok {
		slot = make(chan struct{}, liveMarketParallel)
		c.slots[source.Name] = slot
	}
	entry := &liveEventEntry{done: make(chan struct{})}
	c.known[key] = entry
	go func() {
		defer close(entry.done)
		slot <- struct{}{}
		defer func() { <-slot }()
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveEventFetchTimeout)
		defer cancel()
		entry.events, entry.err = source.Fetch(fetchCtx, client, query)
		entry.at = now()
	}()
}

func (c *liveEventCache) peek(source, term string) *liveEventEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.known[liveEventKey(source, term)]
}

func (c *liveEventCache) collect(wait context.Context, sources []LiveEventSource, term string,
	failed map[string]error,
) (found []LiveAdoptionEvent, answered []string) {
	for _, source := range sources {
		entry := c.peek(source.Name, term)
		if entry == nil {
			continue
		}
		select {
		case <-entry.done:
		case <-wait.Done():
		}
		switch {
		case !entry.finished():
		case entry.err != nil:
			failed[source.Name] = entry.err
		default:
			answered = append(answered, source.Name)
			found = append(found, entry.events...)
		}
	}
	return found, answered
}

// liveEventQueryOf берёт репозитории из аннотаций работ: имя пакета выводится из кода авторов, а не угадывается.
func liveEventQueryOf(topic LiveTopic, metadata map[string]LivePaperMetadata) LiveEventQuery {
	query := LiveEventQuery{Term: liveMarketTerm(topic), Repos: []string{}}
	if liveRussianText(topic.Label) != "" {
		query.Russian = topic.Label
	}
	for _, id := range topic.PaperIDs {
		for _, repository := range liveRepositoryURL.FindAllString(metadata[id].Abstract, -1) {
			repository = strings.TrimSuffix(strings.TrimRight(repository, "."), ".git")
			if !slices.Contains(query.Repos, repository) {
				query.Repos = append(query.Repos, repository)
			}
		}
	}
	return query
}

func (s *TrendLiveService) askEvents(ctx context.Context, topics []LiveTopic, metadata map[string]LivePaperMetadata) {
	sources := s.eventSources()
	if len(sources) == 0 {
		return
	}
	client := s.newSources().http
	for _, topic := range topics {
		query := liveEventQueryOf(topic, metadata)
		if query.Term == "" {
			continue
		}
		for _, source := range sources {
			s.events.fetch(ctx, client, source, query, s.now)
		}
	}
}

// takeEvents: отказ источника — оговорка зонтика, а не падение поиска.
func (s *TrendLiveService) takeEvents(ctx context.Context, umbrella *LiveTopic, topics []LiveTopic,
	metadata map[string]LivePaperMetadata, deadline time.Time,
) bool {
	sources := s.eventSources()
	wait, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	web := os.Getenv("WEB_COLLECTOR_URL") != "" && !slices.ContainsFunc(umbrella.Limits, func(limit LiveLimit) bool {
		return limit.Code == liveLimitChannelOff && strings.HasPrefix(limit.Text, "WEB_COLLECTOR_URL")
	})
	failed := map[string]error{}
	changed := false
	for i := range topics {
		query := liveEventQueryOf(topics[i], metadata)
		if query.Term == "" {
			continue
		}
		found, answered := s.events.collect(wait, sources, query.Term, failed)
		if web {
			answered = append([]string{liveEventWebSource}, answered...)
		}
		topics[i].eventSources = answered
		if liveAttachEvents(&topics[i], found, query) {
			changed = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(failed)) {
		// Источник событий — не патенты и не ленты: код каналов назвал бы недоступными именно их.
		limit := liveLimitFail(liveLimitSourceOff, "источник событий "+name, failed[name])
		if !slices.ContainsFunc(umbrella.Limits, func(have LiveLimit) bool { return have.Text == limit.Text }) {
			umbrella.Limits = append(umbrella.Limits, limit)
		}
	}
	return changed
}

const liveEventWebSource = "web"

// liveAttachEvents собирает список заново: ответ читают, пока опоздавшие события дописываются в копию.
func liveAttachEvents(topic *LiveTopic, found []LiveAdoptionEvent, query LiveEventQuery) bool {
	for _, record := range liveChannelRecords(topic.Web) {
		if liveMarketPublisher(record.PublisherType) {
			found = append(found, LiveAdoptionEvent{Source: liveEventWebSource, Record: record})
		}
	}
	events := slices.Clone(topic.Events)
	seen := map[string]bool{}
	for _, event := range events {
		seen[liveEventIdentityKey(event)] = true
	}
	added := false
	for _, event := range found {
		event.typed = event.typed || event.Type != ""
		event.Record = liveEventPassport(event.Record)
		// Тип счётчика задаёт источник: словарь по заголовку вроде «Transformer (deep learning)» только навредил бы.
		if event.Count == nil {
			event = liveEventFromText(event)
		}
		key := liveEventIdentityKey(event)
		if seen[key] || (event.Type == "" && event.Count == nil && event.Actor == "") ||
			(event.Count == nil && !liveEventNamed(event, query)) {
			continue
		}
		if event.Basis == "" {
			event.Basis = "имя технологии стоит в заголовке или цитате; отдельной проверкой моделью не подтверждено"
		}
		seen[key] = true
		events = append(events, event)
		added = true
	}
	if !added {
		return false
	}
	slices.SortStableFunc(events, liveEventOrder)
	counters := slices.DeleteFunc(slices.Clone(events), func(event LiveAdoptionEvent) bool { return event.Count == nil })
	typed := slices.DeleteFunc(events, func(event LiveAdoptionEvent) bool { return event.Count != nil })
	topic.Events = slices.Concat(counters, typed[:min(len(typed), liveEventKept)])
	return true
}

// liveEventIdentityKey склеивает одну заметку, пришедшую и из ленты, и из Bing.
func liveEventIdentityKey(event LiveAdoptionEvent) string {
	return event.Type + "\n" + strings.ToLower(cmp.Or(event.Record.CanonicalURL, event.Record.URL, event.Record.ID,
		event.Source+event.Record.Title))
}

func liveEventOrder(a, b LiveAdoptionEvent) int {
	verified := func(event LiveAdoptionEvent) int {
		if event.Verified {
			return 1
		}
		return 0
	}
	return cmp.Or(-cmp.Compare(verified(a), verified(b)), -cmp.Compare(LiveEventStage(a.Type), LiveEventStage(b.Type)),
		-cmp.Compare(liveEventDate(a), liveEventDate(b)))
}

func liveEventDate(event LiveAdoptionEvent) string {
	return cmp.Or(event.Record.Date, event.Record.PublishedAt, event.Record.AvailableAt)
}

// liveEventWorks ставит пилоты и контракты первыми: кейс — внедрение у клиента, а научная работа даёт лишь бенчмарк.
func liveEventWorks(topic *LiveTopic) {
	have := map[string]bool{}
	for _, id := range topic.EvidenceIDs {
		have[id] = true
	}
	picked := []LiveSourceRecord{}
	for _, event := range topic.Events {
		record := event.Record
		id := liveEvidenceID(record)
		if len(picked) == liveEventCardRecords || LiveEventStage(event.Type) < 2 || have[id] ||
			record.Title == "" || liveEventDate(event) == "" || cmp.Or(record.ID, record.URL) == "" {
			continue
		}
		have[id] = true
		record.Excerpt = cmp.Or(record.Excerpt, event.Quote)
		picked = append(picked, record)
	}
	if len(picked) == 0 {
		return
	}
	web := LiveChannel{Counts: []*int{}}
	if topic.Web != nil {
		web = *topic.Web
	}
	web.Records = slices.Concat(web.Records, picked)
	web.Total = len(web.Records)
	topic.Web = &web
	ids := make([]string, len(picked))
	for i, record := range picked {
		ids[i] = liveEvidenceID(record)
	}
	topic.EvidenceIDs = slices.Concat(ids, topic.EvidenceIDs)
	topic.marketIDs = append(topic.marketIDs, ids...)
	topic.RecordsCount += len(picked)
	topic.DocumentsCount += len(picked)
}

func liveEventRecordIDs(topic LiveTopic) map[string]bool {
	out := map[string]bool{}
	for _, event := range topic.Events {
		if LiveEventStage(event.Type) >= 2 {
			out[liveEvidenceID(event.Record)] = true
		}
	}
	return out
}

// awaitEvents: опоздавшие события модель не проверяет, и стадию они не меняют.
func (s *TrendLiveService) awaitEvents(ctx context.Context, detail LiveDetail) (LiveDetail, bool) {
	if len(s.eventSources()) == 0 || len(detail.Candidates) == 0 {
		return detail, false
	}
	candidates := slices.Clone(detail.Candidates)
	umbrella := detail.Topic
	umbrella.Limits = slices.Clone(umbrella.Limits)
	if !s.takeEvents(ctx, &umbrella, candidates, nil, time.Now().Add(liveEventFetchTimeout)) {
		return detail, false
	}
	for i := range candidates {
		liveAssessTZ(&candidates[i], nil)
	}
	detail.Candidates, detail.Topic = candidates, umbrella
	return detail, true
}

func (s *TrendLiveService) assessEvents(ctx context.Context, umbrella *LiveTopic, candidates []LiveTopic,
	documents []StudyPaper, metadata map[string]LivePaperMetadata,
) {
	byID := make(map[string]StudyPaper, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	s.takeEvents(ctx, umbrella, candidates, metadata, time.Now())
	for i := range candidates {
		liveAssessTZ(&candidates[i], byID)
	}
}
