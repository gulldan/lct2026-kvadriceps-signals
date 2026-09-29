package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// Условия Bing — личное некоммерческое использование; источник подключён решением владельца продукта.

const (
	liveEventSourceBing  = "bing"
	liveEventSourceWebRU = "web_ru"
	liveBingNewsURL      = "https://www.bing.com/news/search"
	// Ограничение по сайтам Bing выполняет только на рынке en-US: на ru-RU тот же запрос пуст.
	liveRussianSites = "(site:tadviser.ru OR site:cnews.ru OR site:habr.com)"
	// Кейсу нужна одна запись, остальное — запас на проверку моделью.
	liveRussianEvents = 5
)

func liveNameHead(name string) string {
	var b strings.Builder
	depth := 0
	for _, r := range name {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	head := b.String()
	for _, cut := range []string{" — ", " – ", ": "} {
		head, _, _ = strings.Cut(head, cut)
	}
	return strings.Join(strings.Fields(head), " ")
}

// liveNameCore укорачивает запрос, не ослабляя отбор: он всё равно идёт по основам всего названия.
func liveNameCore(name string) string {
	head := liveNameHead(name)
	for _, cut := range []string{" для ", " на основе ", " в ", " во ", " на ", " при ", " for "} {
		if core, _, ok := strings.Cut(head, cut); ok && len(liveWords(core)) >= 2 {
			head = core
		}
	}
	return head
}

// liveRussianName: английское имя тоже ищется на русских сайтах — о LangGraph и MCP пишут латиницей.
type liveRussianName struct {
	query string
	roots []string
}

func liveRussianNames(query LiveEventQuery) []liveRussianName {
	names := []liveRussianName{}
	for _, name := range []string{query.Russian, query.Term} {
		core, roots := liveNameCore(name), liveRoots(liveNameHead(name))
		if core == "" || len(roots) == 0 || !liveDistinctName(core) || slices.ContainsFunc(names, func(known liveRussianName) bool {
			return strings.EqualFold(known.query, core)
		}) {
			continue
		}
		// Латиница в кавычках, иначе «Open RAN» совпадёт с любым «open»; русская без — чтобы искались все формы слов.
		if liveTextScript(core) != "ru" {
			core = `"` + core + `"`
		}
		names = append(names, liveRussianName{query: core, roots: roots})
	}
	return names
}

// liveDistinctName: однословное английское имя — только имя продукта, «Mamba» на русских сайтах — служба знакомств.
func liveDistinctName(name string) bool {
	if len(liveWords(name)) > 1 || liveTextScript(name) == "ru" {
		return true
	}
	var previous rune
	for _, r := range name {
		if r >= '0' && r <= '9' || r == '.' || previous >= 'a' && previous <= 'z' && r >= 'A' && r <= 'Z' {
			return true
		}
		previous = r
	}
	return false
}

func liveNamedBy(text string, names []liveRussianName) bool {
	return slices.ContainsFunc(names, func(name liveRussianName) bool { return liveNamed(text, name.roots) })
}

type liveBingFeed struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
	} `xml:"channel>item"`
}

// liveBingRecords: упоминание в глубине обзора кейсом не служит — название должно стоять в заголовке или фрагменте.
func liveBingRecords(body []byte, names []liveRussianName) ([]LiveSourceRecord, error) {
	var feed liveBingFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("bing: parse: %w", err)
	}
	records := []LiveSourceRecord{}
	for _, item := range feed.Items {
		link, err := url.Parse(item.Link)
		if err != nil {
			continue
		}
		address := link.Query().Get("url")
		title := strings.Join(strings.Fields(item.Title), " ")
		text := strings.Join(strings.Fields(item.Description), " ")
		published, err := time.Parse(time.RFC1123, item.PubDate)
		web := strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
		if !web || err != nil || liveTextScript(title+" "+text) != "ru" || !liveNamedBy(title+" "+text, names) {
			continue
		}
		records = append(records, liveEventRecord(LiveSourceRecord{
			ID: address, Title: title, Date: published.UTC().Format(time.DateOnly), URL: address, Source: "Bing News",
			PublishedAt: published.UTC().Format(time.RFC3339), TitleBasis: "заголовок в выдаче Bing News",
			DateBasis: "дата материала в выдаче Bing News", Excerpt: text,
		}, title+" "+text))
	}
	return records, nil
}

// liveRussianEventsOf ставит первыми записи с типом по словарю конвейера: без типа он их не возьмёт.
func liveRussianEventsOf(source string, records []LiveSourceRecord, names []liveRussianName) []LiveAdoptionEvent {
	typed := func(record LiveSourceRecord) int {
		if liveEventTypeOf(record.Title+". "+record.Excerpt) != "" {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(records, func(a, b LiveSourceRecord) int {
		return cmp.Or(-cmp.Compare(typed(a), typed(b)), -cmp.Compare(a.Date, b.Date))
	})
	events := make([]LiveAdoptionEvent, 0, min(len(records), liveRussianEvents))
	for _, record := range records[:min(len(records), liveRussianEvents)] {
		event := LiveAdoptionEvent{Source: source, Record: record}
		if !liveNamedBy(record.Title, names) {
			event.Quote = cmp.Or(record.Excerpt, record.Title)
		}
		events = append(events, event)
	}
	return events
}

func liveRussianBingEvents(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	src := &liveSources{http: client}
	names := liveRussianNames(query)
	found := []LiveSourceRecord{}
	for _, name := range names {
		values := url.Values{"format": {"rss"}, "mkt": {"en-US"}, "count": {"50"}, "qft": {`sortbydate="1"`},
			"q": {name.query + " " + liveRussianSites}}
		body, _, err := liveEventFetch(ctx, src, liveBingNewsURL+"?"+values.Encode(), nil)
		if err != nil {
			return liveRussianEventsOf(liveEventSourceBing, found, names), fmt.Errorf("bing: %w", err)
		}
		records, err := liveBingRecords(body, names)
		if err != nil {
			return liveRussianEventsOf(liveEventSourceBing, found, names), err
		}
		for _, record := range records {
			if !slices.ContainsFunc(found, func(have LiveSourceRecord) bool { return have.URL == record.URL }) {
				found = append(found, record)
			}
		}
	}
	return liveRussianEventsOf(liveEventSourceBing, found, names), nil
}

func liveRussianFeedEvents(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	base := strings.TrimRight(os.Getenv("WEB_COLLECTOR_URL"), "/")
	names := liveRussianNames(query)
	if base == "" || len(names) == 0 {
		return nil, nil
	}
	terms := []string{}
	for _, name := range names {
		if term := liveRussianFeedTerm(name.query); term != "" && !slices.Contains(terms, term) {
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return nil, errors.New("web: название не помещается в поисковую фразу сборщика")
	}
	now := time.Now().UTC()
	periods := []LivePeriod{{ID: "year", Start: now.AddDate(-1, 0, 0).Format(time.DateOnly),
		EndExclusive: now.AddDate(0, 0, 1).Format(time.DateOnly)}}
	body, err := json.Marshal(struct {
		Terms   []string     `json:"terms"`
		Periods []LivePeriod `json:"periods"`
		Titles  bool         `json:"titles"`
	}{terms, periods, true})
	if err != nil {
		return nil, fmt.Errorf("web: encode query: %w", err)
	}
	channel, err := (&liveSources{http: client}).queryChannelAt(ctx, base+"/v1/query", liveChannelBytes, body, len(periods))
	if err != nil {
		return nil, fmt.Errorf("web: %w", err)
	}
	// Сборщик ищет по ядру названия, поэтому заголовок с фрагментом должен называть технологию целиком.
	records := slices.DeleteFunc(channel.Records, func(record LiveSourceRecord) bool {
		return liveTextScript(record.Title) != "ru" || !liveNamedBy(record.Title+" "+record.Excerpt, names)
	})
	for i := range records {
		if records[i].Language == "" {
			records[i].Language, records[i].LanguageBasis = "ru", "определён по преобладающему алфавиту заголовка"
		}
	}
	return liveRussianEventsOf(liveEventSourceWebRU, records, names), nil
}

// Сборщик ограничивает байты, а не буквы: поиск сокращается по словам, проверка записей сохраняет полное имя.
func liveRussianFeedTerm(name string) string {
	words := strings.Fields(strings.Trim(name, `"`))
	size, count := 0, 0
	for i, word := range words {
		if size+i+len(word) > 200 {
			break
		}
		size += len(word)
		count++
	}
	return strings.Join(words[:count], " ")
}
