package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Зрелость — по стандарту IETF и давней статье Википедии: у ISO и W3C открытого поиска по заголовку нет.

const (
	liveEventIETF      = "ietf"
	liveEventWikipedia = "wikipedia"
	liveIETFBase       = "https://datatracker.ietf.org"
	liveWikiBase       = "https://en.wikipedia.org/w/api.php"
	liveWikiViewsBase  = "https://wikimedia.org/api/rest_v1/metrics/pageviews/per-article/en.wikipedia/all-access/user/"
	// liveIETFStandards ограничивает число RFC, датируемых отдельным запросом.
	liveIETFStandards = 3
	liveEventBody     = 4 << 20
	// Wikimedia требует в User-Agent адрес для связи с владельцем клиента.
	liveEventAgent = "rag-test-signalgraph/1.0 (+https://github.com/gulldan/demo-rag)"
)

func liveEventGet(ctx context.Context, client *http.Client, endpoint string, into any) error {
	body, err := liveMarketFetch(ctx, client, http.MethodGet, endpoint, nil, nil, liveEventBody)
	var status *liveMarketStatusError
	if errors.As(err, &status) && status.status == http.StatusNotFound {
		return errLiveEventNotFound
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return nil
}

var errLiveEventNotFound = errors.New("not found")

// liveIETFSource берёт только RFC на пути стандартизации: информационных у «post-quantum cryptography» больше.
func liveIETFSource(now func() time.Time) LiveEventSource {
	return LiveEventSource{Name: liveEventIETF, Fetch: func(ctx context.Context, client *http.Client,
		query LiveEventQuery,
	) ([]LiveAdoptionEvent, error) {
		var found struct {
			Objects []struct {
				Name     string `json:"name"`
				Title    string `json:"title"`
				Abstract string `json:"abstract"`
				StdLevel string `json:"std_level"`
			} `json:"objects"`
		}
		term := liveEventTitleTerm(query.Term)
		if term == "" {
			return []LiveAdoptionEvent{}, nil
		}
		endpoint := liveIETFBase + "/api/v1/doc/document/?format=json&limit=50&type=rfc&title__icontains=" +
			url.QueryEscape(term)
		if err := liveEventGet(ctx, client, endpoint, &found); err != nil {
			return nil, fmt.Errorf("ietf: %w", err)
		}
		events := []LiveAdoptionEvent{}
		for _, document := range found.Objects {
			level := path.Base(strings.TrimSuffix(document.StdLevel, "/"))
			if !slices.Contains([]string{"ps", "ds", "std", "bcp"}, level) || len(events) == liveIETFStandards {
				continue
			}
			var published struct {
				Objects []struct {
					Time string `json:"time"`
				} `json:"objects"`
			}
			// Дата у документа — время последней правки записи, а не выхода RFC.
			_ = liveEventGet(ctx, client, liveIETFBase+"/api/v1/doc/docevent/?format=json&type=published_rfc&doc__name="+
				url.QueryEscape(document.Name), &published)
			date := ""
			if len(published.Objects) > 0 {
				date = published.Objects[0].Time[:min(len(published.Objects[0].Time), 10)]
			}
			link := liveIETFBase + "/doc/" + document.Name + "/"
			events = append(events, LiveAdoptionEvent{Type: LiveEventStandard, Source: liveEventIETF, Actor: "IETF",
				Phase: strings.ToUpper(document.Name), Quote: document.Title, typed: true,
				Record: LiveSourceRecord{ID: liveEventIETF + ":" + document.Name, Title: document.Title, Date: date,
					URL: link, Source: "IETF Datatracker", Language: "en", Excerpt: document.Abstract,
					ObservedAt: now().UTC().Format(time.RFC3339)}})
		}
		return events, nil
	}}
}

// liveWikipediaSource без статьи отвечает пусто, а не отказом: отсутствие статьи — тоже измерение.
func liveWikipediaSource(now func() time.Time) LiveEventSource {
	return LiveEventSource{Name: liveEventWikipedia, Fetch: func(ctx context.Context, client *http.Client,
		query LiveEventQuery,
	) ([]LiveAdoptionEvent, error) {
		title := liveEventTitleTerm(query.Term)
		if title == "" {
			return []LiveAdoptionEvent{}, nil
		}
		title = strings.ToUpper(title[:1]) + title[1:]
		var page struct {
			Query struct {
				Pages []struct {
					Title       string `json:"title"`
					Description string `json:"description"`
					Missing     bool   `json:"missing"`
					Revisions   []struct {
						Timestamp string `json:"timestamp"`
					} `json:"revisions"`
				} `json:"pages"`
			} `json:"query"`
		}
		endpoint := liveWikiBase + "?action=query&prop=revisions|description&rvlimit=1&rvdir=newer&rvprop=timestamp" +
			"&redirects=1&format=json&formatversion=2&titles=" + url.QueryEscape(title)
		if err := liveEventGet(ctx, client, endpoint, &page); err != nil {
			return nil, fmt.Errorf("wikipedia: %w", err)
		}
		if len(page.Query.Pages) == 0 || page.Query.Pages[0].Missing || len(page.Query.Pages[0].Revisions) == 0 ||
			!liveWikiTechnology(title, page.Query.Pages[0].Description) {
			return []LiveAdoptionEvent{}, nil
		}
		article := page.Query.Pages[0]
		month := now().UTC().AddDate(0, -1, 0)
		first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1)
		var views struct {
			Items []struct {
				Views int `json:"views"`
			} `json:"items"`
		}
		name := url.PathEscape(strings.ReplaceAll(article.Title, " ", "_"))
		err := liveEventGet(ctx, client, liveWikiViewsBase+name+"/monthly/"+first.Format("20060102")+"00/"+
			last.Format("20060102")+"00", &views)
		if err != nil && !errors.Is(err, errLiveEventNotFound) {
			return nil, fmt.Errorf("wikipedia pageviews: %w", err)
		}
		count := 0
		for _, item := range views.Items {
			count += item.Views
		}
		created := article.Revisions[0].Timestamp
		return []LiveAdoptionEvent{{Source: liveEventWikipedia, Count: &count, Unit: "views", typed: true,
			Basis: fmt.Sprintf("статья Википедии: создана %s, просмотров за %s — %d; признак зрелости рынка, "+
				"не вход модели", created[:min(len(created), 10)], first.Format("2006-01"), count),
			Record: LiveSourceRecord{ID: liveEventWikipedia + ":" + article.Title, Title: article.Title,
				Date: created[:min(len(created), 10)], URL: "https://en.wikipedia.org/wiki/" + name,
				Source: "Wikipedia", Language: "en", ObservedAt: now().UTC().Format(time.RFC3339)}}}, nil
	}}
}

// liveWikiTechnology отсекает однословные совпадения с чужим предметом («Mamba» — о змеях): такая статья
// принимается, только если её описание — о технологии.
func liveWikiTechnology(title, description string) bool {
	return len(strings.Fields(title)) > 1 || liveWikiTechWords.MatchString(description)
}

var liveWikiTechWords = regexp.MustCompile(`(?i)\b(software|algorithm|comput|technolog|protocol|data|digital|network|` +
	`learning|neural|model|machine|payment|cryptograph|robot|battery|electronic|semiconductor|chip|platform|standard|` +
	`programming|language model|artificial intelligence|encryption|blockchain|cloud|sensor|internet)`)

func liveEventPassport(record LiveSourceRecord) LiveSourceRecord {
	if record.PublisherType != "" && record.Trust != "" {
		return record
	}
	link := cmp.Or(record.URL, record.CanonicalURL)
	passport, genre := liveApplyGenre(ClassifySource(link), link, record.Title)
	record.PublisherType = cmp.Or(record.PublisherType, passport.Publisher)
	record.Trust = cmp.Or(record.Trust, passport.Trust)
	record.TrustBasis = cmp.Or(record.TrustBasis, passport.TrustBasis)
	record.Genre = cmp.Or(record.Genre, genre)
	return record
}

// liveEventTitleTerm убирает аббревиатуру в скобках: «model context protocol (mcp)» не совпадает ни с одним
// заголовком.
func liveEventTitleTerm(term string) string {
	return strings.Join(strings.Fields(liveEventParenthetical.ReplaceAllString(term, " ")), " ")
}

var liveEventParenthetical = regexp.MustCompile(`\([^)]*\)`)
