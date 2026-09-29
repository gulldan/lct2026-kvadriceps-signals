package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/mmcdole/gofeed/atom"
)

const webFeedParserVersion = "gofeed@v1.4.2+publication-v4"
const kindFeed = "feed"

type feed struct{ URL, Name string }

func defaultFeeds() []feed {
	return []feed{
		{"https://news.mit.edu/rss/topic/energy", "MIT Energy"},
		{"https://news.mit.edu/rss/topic/materialsscienceandengineering", "MIT Materials"},
		{"https://news.mit.edu/rss/topic/biology", "MIT Biotechnology"},
		{"https://news.mit.edu/rss/topic/manufacturing", "MIT Manufacturing"},
		{"https://news.mit.edu/rss/topic/artificial-intelligence2", "MIT AI"},
		{"https://news.mit.edu/rss/topic/health-sciences-and-technology", "MIT Health"},
		{"https://www.nasa.gov/feed/", "NASA"},
	}
}

// Разбор Atom в gofeed подставляет updated вместо отсутствующего published — для тренда это выдуманная
// дата первой публикации. Переопределено только это сопоставление.
type publicationAtomTranslator struct{}

func (publicationAtomTranslator) Translate(value any) (*gofeed.Feed, error) {
	parsed, err := (&gofeed.DefaultAtomTranslator{}).Translate(value)
	if err != nil {
		return nil, fmt.Errorf("translate Atom publication: %w", err)
	}
	feed, ok := value.(*atom.Feed)
	if !ok || len(feed.Entries) != len(parsed.Items) {
		return nil, errors.New("invalid Atom translation")
	}
	for i, entry := range feed.Entries {
		parsed.Items[i].Published, parsed.Items[i].PublishedParsed = entry.Published, entry.PublishedParsed
	}
	return parsed, nil
}

func feedRows(body []byte, source feed, through string) ([]record, error) {
	if len(body) > 8<<20 {
		return nil, errors.New("feed exceeds 8 MiB")
	}
	parser := gofeed.NewParser()
	parser.AtomTranslator = publicationAtomTranslator{}
	parsed, err := parser.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode publication feed: %w", err)
	}
	// Язык берём из ленты: разбор статьи ставит его, только если страница объявляет язык сама.
	language, languageBasis := feedLanguage(parsed.Language)
	var rows []record
	seen := map[string]bool{}
	for _, item := range parsed.Items {
		if item == nil {
			continue
		}
		published := cmp.Or(item.PublishedParsed, feedLooseDate(item.Published))
		if published == nil || published.IsZero() {
			continue
		}
		// Календарная дата — в поясе издателя (полночь в Японии — тот же день); точный момент — в PublishedAt в UTC.
		date := *published
		day := sourceDay(item.Published)
		var limitations []string
		if day == "" {
			day = date.UTC().Format(time.DateOnly)
			limitations = []string{"publisher_calendar_date_unparsed; calendar date derived from parsed UTC instant"}
		}
		link := absolutePublic(item.Link, source.URL)
		title := strings.Join(strings.Fields(textHTML(item.Title)), " ")
		if link == "" || seen[link] || title == "" || day > through {
			continue
		}
		seen[link] = true
		basis := "RSS pubDate or Dublin Core date"
		switch parsed.FeedType {
		case "atom":
			basis = "Atom published"
		case "json":
			basis = "JSON Feed date_published"
		}
		text, status := feedItemText(item)
		rows = append(rows, record{ID: link, Title: title, Date: day, DateBasis: basis, Limitations: limitations,
			PublishedAt: date.UTC().Format(time.RFC3339), Text: text, TextStatus: status,
			ParserVersion: webFeedParserVersion, TitleBasis: "publisher feed title", URL: link, Source: source.Name,
			Language: language, LanguageBasis: languageBasis})
	}
	if len(rows) == 0 {
		return nil, errors.New("publication feed contains no usable explicitly dated items")
	}
	return rows, nil
}

// feedLooseDate читает «Sep 22, 2026 8:39pm» платформы Fierce; пояса издатель не называет, день берётся как написан.
func feedLooseDate(value string) *time.Time {
	for _, layout := range []string{"Jan 2, 2006 3:04pm", "Jan 2, 2006"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return &parsed
		}
	}
	return nil
}

// feedFullTextRunes — с какой длины текст записи — статья, а не анонс; feedTextRunes не даёт ленте
// с тяжёлой разметкой раздуть снимок.
const (
	feedFullTextRunes = 1500
	feedTextRunes     = 20000
)

// feedItemText: страницы части изданий сеть стенда обрывает или закрывает от роботов, а ленту с content:encoded — нет.
func feedItemText(item *gofeed.Item) (string, string) {
	text := strings.TrimSpace(textHTML(item.Content))
	if summary := strings.TrimSpace(textHTML(item.Description)); len(summary) > len(text) {
		text = summary
	}
	runes := []rune(text)
	switch {
	case len(runes) >= feedFullTextRunes:
		return string(runes[:min(len(runes), feedTextRunes)]), articleText
	case len(runes) > 0:
		return text, feedAbstractText
	}
	return "", metadataOnlyText
}

const feedAbstractText = "abstract_only"

// feedLanguage не угадывает язык: неверный язык оригинала рядом со ссылкой хуже пустого.
func feedLanguage(declared string) (code string, basis string) {
	value := strings.ToLower(strings.TrimSpace(declared))
	if value == "" {
		return "", ""
	}
	if cut, _, found := strings.Cut(value, "-"); found {
		value = cut
	}
	if len(value) != 2 {
		return "", ""
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			return "", ""
		}
	}
	return value, "publisher feed language"
}
