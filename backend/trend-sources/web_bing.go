package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// Bing News закрывает длинный хвост эталона: его ссылки разбросаны по двум сотням сайтов. Условия Bing
// разрешают выдачу только для личных некоммерческих целей; источник подключён по решению владельца продукта.

const (
	kindBingNews      = "bing_news"
	bingParserVersion = "gofeed@v1.4.2+bing-news-v1"
	// bingTermsKept: проход спрашивает каждое имя, и больше трёхсот съели бы заметную долю бюджета запросов.
	bingTermsKept = 300
	// bingLiveTimeout: выдача нужна карточке, а не самому поиску, и медленный ответ не должен его задерживать.
	bingLiveTimeout = 8 * time.Second
)

// bingAddress просит свежие первыми: по релевантности приходят статьи прошлых лет, мимо окна поиска.
func bingAddress(base, term string) string {
	return base + "?format=rss&mkt=en-US&count=50&qft=" + url.QueryEscape(`sortbydate="1"`) + "&q=" +
		url.QueryEscape(`"`+term+`"`)
}

// bingRows ставит адрес издания, а не переадресацию Bing: по нему ставится паспорт и склеиваются дубли.
// Имя ищется только в заголовке и вводном абзаце: упоминание в глубине обзора кейсом не служит.
func bingRows(body []byte, source webSource, term, through string) ([]record, error) {
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode Bing News feed: %w", err)
	}
	rows := []record{}
	seen := map[string]bool{}
	for _, item := range parsed.Items {
		if item == nil || item.PublishedParsed == nil || item.PublishedParsed.IsZero() {
			continue
		}
		link := bingOriginal(item.Link)
		title := strings.Join(strings.Fields(textHTML(item.Title)), " ")
		text := strings.TrimSpace(textHTML(item.Description))
		day := item.PublishedParsed.UTC().Format(time.DateOnly)
		if link == "" || seen[link] || title == "" || day > through || !matches(title+" "+text, []string{term}) {
			continue
		}
		seen[link] = true
		status := feedAbstractText
		if text == "" {
			status = metadataOnlyText
		}
		rows = append(rows, record{ID: link, Title: title, Date: day, DateBasis: "Bing News pubDate",
			PublishedAt: item.PublishedParsed.UTC().Format(time.RFC3339), Text: text, TextStatus: status,
			ParserVersion: bingParserVersion, TitleBasis: "Bing News title", URL: link, Source: source.Name,
			Industry: source.Industry})
	}
	return rows, nil
}

func bingOriginal(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if original := u.Query().Get("url"); original != "" {
		return absolutePublic(original, original)
	}
	return ""
}

func (s *service) bingSource() (webSource, bool) {
	at := slices.IndexFunc(s.config.Web, func(source webSource) bool { return source.Kind == kindBingNews })
	if at < 0 {
		return webSource{}, false
	}
	return s.config.Web[at], true
}

// bingHistory повторяет запомненные имена, чтобы найденное живым поиском пережило вытеснение из выдачи Bing.
func (s *service) bingHistory(ctx context.Context, source webSource, data *snapshot) {
	terms := s.bingTerms()
	c := coverage{Source: source.Name, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: fmt.Sprintf("Bing News: %d remembered technology names queried; results are Bing's selection, "+
			"not a census of the press", len(terms))}
	seen := map[string]bool{}
	for _, term := range terms {
		address := bingAddress(source.URL, term)
		raw, err := s.fetch(ctx, address, data)
		if errors.Is(err, errBudget) || ctx.Err() != nil {
			c.StopReason = "request budget or pass deadline reached before every remembered name was queried"
			break
		}
		if err != nil {
			c.Limitations = append(c.Limitations, term+": "+err.Error())
			continue
		}
		rows, err := bingRows(raw, source, term, data.Through)
		if err != nil {
			c.Limitations = append(c.Limitations, term+": "+err.Error())
			continue
		}
		page := provenance{URL: address, SHA256: hashBytes(raw)}
		c.Pages = append(c.Pages, page)
		for _, row := range rows {
			if row.Date >= data.From && !seen[row.ID] {
				seen[row.ID] = true
				row.Provenance = []provenance{page}
				data.Records = append(data.Records, row)
			}
		}
	}
	data.Coverage = append(data.Coverage, c)
}

// bingLive идёт в обход бюджета прохода: запрос один на имя и не трогает состояние идущего прохода.
func (s *service) bingLive(ctx context.Context, source webSource, term string) []record {
	address, err := url.Parse(bingAddress(source.URL, term))
	if err != nil || allowedAddress(address, s.config.domains()) != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, bingLiveTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), http.NoBody)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0 (+public research metadata)")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil
	}
	raw = sourceUTF8(raw, address.String(), resp.Header.Get("Content-Type"), &snapshot{})
	rows, err := bingRows(raw, source, term, time.Now().UTC().Format(time.DateOnly))
	if err != nil {
		return nil
	}
	return rows
}

func (s *service) bingTermsPath() string { return filepath.Join(s.dir, "bing-terms.json") }

func (s *service) bingTerms() []string {
	s.bingMu.Lock()
	defer s.bingMu.Unlock()
	return s.loadBingTerms()
}

func (s *service) loadBingTerms() []string {
	raw, err := os.ReadFile(s.bingTermsPath())
	var terms []string
	if err != nil || json.Unmarshal(raw, &terms) != nil {
		return nil
	}
	return terms
}

func (s *service) rememberBingTerm(term string) error {
	s.bingMu.Lock()
	defer s.bingMu.Unlock()
	terms := slices.DeleteFunc(s.loadBingTerms(), func(known string) bool { return known == term })
	terms = append([]string{term}, terms...)
	return atomicSourceJSON(s.bingTermsPath(), terms[:min(len(terms), bingTermsKept)])
}

// withBingNews спрашивает только по имени кандидата: широкие термины зонтичного поиска дали бы шум.
func (s *service) withBingNews(ctx context.Context, q request, records []record, counts []*int) []record {
	source, ok := s.bingSource()
	if !ok || !q.Titles || len(q.Terms) != 1 {
		return records
	}
	if err := s.rememberBingTerm(q.Terms[0]); err != nil {
		slog.Warn("remember Bing News term", "error", err)
	}
	have := map[string]bool{}
	for _, row := range records {
		have[row.ID] = true
	}
	for _, row := range s.bingLive(ctx, source, q.Terms[0]) {
		i := slices.IndexFunc(q.Periods, func(p period) bool { return row.Date >= p.Start && row.Date < p.End })
		if i < 0 || have[row.ID] {
			continue
		}
		have[row.ID] = true
		if counts[i] == nil {
			counts[i] = new(0)
		}
		*counts[i]++
		records = append(records, queryPreview(row, q))
	}
	return records
}
