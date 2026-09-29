package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type wpRow struct {
	ID     int    `json:"id"`
	Date   string `json:"date_gmt"`
	Link   string `json:"link"`
	Status string `json:"status"`
	Title  struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Content struct {
		Rendered  string `json:"rendered"`
		Protected bool   `json:"protected"`
	} `json:"content"`
}

func wordpressRows(raw []byte, source webSource, window period, observed string) ([]record, int, error) {
	var entries []wpRow
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, 0, fmt.Errorf("decode publisher page: %w", err)
	}
	rows := []record{}
	for _, entry := range entries {
		day := sourceDay(entry.Date)
		link := absolutePublic(entry.Link, source.URL)
		if day == "" || day < window.Start || day >= window.End || link == "" || entry.Status != "publish" {
			continue
		}
		title := textHTML(entry.Title.Rendered)
		if title == "" {
			continue
		}
		r := record{ID: link, URL: link, CanonicalURL: link, Title: title, Date: day, DateBasis: "WordPress date_gmt",
			PublishedAt: entry.Date + "Z", Source: source.Name, Industry: source.Industry, ObservedAt: observed,
			AvailableAt: observed, TitleBasis: "publisher REST record", TextStatus: metadataOnlyText}
		if !entry.Content.Protected {
			// content.rendered — фрагмент издателя: экстрактор тот же, но пути фрагмента не выдаются за локаторы страницы.
			doc, parseErr := html.Parse(strings.NewReader(entry.Content.Rendered))
			if parseErr != nil {
				return nil, 0, fmt.Errorf("parse WordPress content: %w", parseErr)
			}
			r.Text, _, r.ParserVersion = articleBody(doc, articleFields{body: doc}, link)
			r.ParserVersion = "wordpress-rest+" + r.ParserVersion
			if len(r.Text) > 0 && len(r.Text) <= 256<<10 {
				r.TextStatus = articleText
				r.ContentHash = hashBytes([]byte(r.Text))
			} else {
				r.Text = ""
			}
		}
		rows = append(rows, r)
	}
	return rows, len(entries), nil
}

// wordpressPage сообщает, остались ли у месяца страницы.
func (s *service) wordpressPage(ctx context.Context, source webSource, window period, page int,
	data *snapshot, c *coverage, received *int) bool {
	address := wordpressPageURL(source.URL, window, page)
	// Текущий месяц обновляется раз в сутки по UTC, завершённые неизменны; смена конфигурации меняет хеш состава.
	identity := data.SourceSet + ":" + window.Start
	if c.Through == data.Through {
		identity += ":" + data.Through
	}
	raw, err := s.cached(ctx, address, identity, data)
	if err != nil {
		status, ok := errors.AsType[*sourceHTTPError](err)
		if ok && status.Code == "rest_post_invalid_page_number" && page > 1 {
			c.Exhausted = true
			c.StopReason = "publisher month pagination exhausted"
			c.Total = new(*received)
			return false
		}
		c.Cursor = address
		c.StopReason = err.Error()
		return false
	}
	provenanceEntry := provenance{URL: address, SHA256: hashBytes(raw)}
	c.Pages = append(c.Pages, provenanceEntry)
	rows, n, err := wordpressRows(raw, source, window, data.ObservedAt)
	if err != nil {
		c.StopReason = err.Error()
		return false
	}
	for i := range rows {
		rows[i].Provenance = []provenance{provenanceEntry}
	}
	data.Records = append(data.Records, rows...)
	*received += n
	if n >= 20 {
		return true
	}
	c.Exhausted = true
	c.StopReason = "publisher month pagination exhausted"
	c.Total = new(*received)
	return false
}

func wordpressPageURL(base string, window period, page int) string {
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("per_page", "20")
	q.Set("page", strconv.Itoa(page))
	start, _ := time.Parse(time.DateOnly, window.Start)
	q.Set("after", start.Add(-time.Second).Format("2006-01-02T15:04:05"))
	q.Set("before", window.End+"T00:00:00")
	q.Set("order", "asc")
	q.Set("orderby", "id")
	q.Set("_fields", "id,date_gmt,link,status,title,content")
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *service) wordpressHistory(ctx context.Context, source webSource, data *snapshot) {
	windows := monthWindows(data.From, data.Through)
	slices.Reverse(windows)
	for _, month := range windows {
		end, _ := time.Parse(time.DateOnly, month.End)
		c := coverage{Source: source.Name, From: month.Start, Through: end.AddDate(0, 0, -1).Format(time.DateOnly),
			ObservedAt: data.ObservedAt, StopReason: textNotCollected}
		received := 0
		for page := 1; page <= 1000; page++ {
			if !s.wordpressPage(ctx, source, month, page, data, &c, &received) {
				break
			}
		}
		data.Coverage = append(data.Coverage, c)
	}
}
