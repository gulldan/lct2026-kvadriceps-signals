package application

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/example/main-service/internal/platform/jsonx"
)

// Событием внедрения считаются только Show HN и Launch HN — запуск продукта авторами, а не обсуждение.

const (
	liveEventSourceHN = "hn"
	liveHNEventHits   = 30
	liveGitHubHost    = "github.com"
)

type liveHNEventSearch struct {
	Hits []struct {
		ID      string   `json:"objectID"`
		Title   string   `json:"title"`
		URL     string   `json:"url"`
		Created string   `json:"created_at"`
		Tags    []string `json:"_tags"`
	} `json:"hits"`
}

// liveHNLaunch разбирает заголовок «Launch HN: Компания (YC S26) – о чём».
func liveHNLaunch() *regexp.Regexp {
	return regexp.MustCompile(`^(?:Launch|Show) HN:\s*([^(–—:,]+?)\s*(?:\((YC [A-Z]\d{2})\))?\s*(?:[–—:,-]\s|$)`)
}

func liveHNEvents(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	// Все слова термина Algolia требует и без кавычек; точное совпадение проверяет цитата, а не поиск.
	address := liveHNBase + "/search?query=" + url.QueryEscape(query.Term) + "&" + url.Values{
		"tags": {"(show_hn,launch_hn)"}, "attributesToHighlight": {""},
		"attributesToRetrieve": {"title,url,created_at,_tags"},
	}.Encode() + "&hitsPerPage=" + strconv.Itoa(liveHNEventHits)
	body, err := liveMarketFetch(ctx, client, http.MethodGet, address, nil, nil, liveMarketMaxJSON)
	if err != nil {
		return nil, fmt.Errorf("hn: %w", err)
	}
	return liveParseHNEvents(body, query.Term)
}

func liveParseHNEvents(body []byte, term string) ([]LiveAdoptionEvent, error) {
	var result liveHNEventSearch
	if err := jsonx.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("hn: parse: %w", err)
	}
	launch := liveHNLaunch()
	events := []LiveAdoptionEvent{}
	for _, hit := range result.Hits {
		// Только заголовок: в тексте поста технология мелькает мимоходом, а запуск должен быть запуском её самой.
		quote := liveTermSentence(hit.Title, term)
		if quote == "" || len(hit.Created) < 10 {
			continue
		}
		eventType := LiveEventProductLaunch
		if host := passportHost(hit.URL); host == liveGitHubHost || host == "gitlab.com" {
			eventType = LiveEventCodeRelease
		}
		match := launch.FindStringSubmatch(hit.Title)
		actor, phase := "", "Show HN"
		if slices.Contains(hit.Tags, "launch_hn") {
			phase = "Launch HN"
		}
		// Заголовок без разделителя — это фраза автора, а не имя продукта.
		if match != nil && len(strings.Fields(match[1])) <= 4 {
			actor, phase = strings.TrimSpace(match[1]), cmp.Or(match[2], phase)
		}
		record := liveMarketRecord("hn:"+hit.ID, hit.Title, hit.Created[:10],
			"https://news.ycombinator.com/item?id="+hit.ID, liveEventSourceHN, "дата публикации на Hacker News")
		record.OriginalURL = hit.URL
		events = append(events, LiveAdoptionEvent{Type: eventType, Source: liveEventSourceHN, Record: record,
			Actor: actor, Phase: phase, Quote: quote})
	}
	return events, nil
}
