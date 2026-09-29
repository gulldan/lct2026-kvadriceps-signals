package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var cnSitemapFile = regexp.MustCompile(`^(\d{4})-W(\d{2})(?:-p\d+)?\.html$`)

const kindCNPatents = "cn-patents"

func patentCollectorKind(kind string) bool { return kind == kindPatents || kind == kindCNPatents }

type cnCandidate struct{ ID, Title string }

// robots.txt Google разрешает /sitemap/ и /patent/, но запрещает поиск, поэтому обход идёт по недельной
// карте сайта. Неделя карты — пакет индекса, а не дата публикации, и полного охвата CN не доказывает.
func (s *service) cnHistory(ctx context.Context, data *snapshot) {
	if s.config.CNDocuments == 0 {
		return
	}
	c := coverage{Source: cnSource, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: "bounded CN discovery from the available Google Patents sitemap"}
	defer func() { data.Coverage = append(data.Coverage, c) }()
	data.Limits = append(data.Limits, "CN coverage is partial: Google Patents sitemap batches may lag publication. "+
		"Candidates are selected by configured terms in sitemap titles, then Chinese originals are read. "+
		"Search, CNIPA authentication, worldwide families and complete CN publication counts are outside this collection.")
	rootURL := cnBase + "/sitemap/"
	raw, err := s.cnPage(ctx, rootURL, "cn-index:"+data.Through, data, &c)
	if err != nil {
		c.Cursor, c.StopReason = rootURL, err.Error()
		return
	}
	weeks, _ := cnSitemap(raw, data.From, data.Through, nil)
	if len(weeks) == 0 {
		c.StopReason = "no sitemap batch within the history window; publication coverage is unknown"
		return
	}
	c.Limitations = append(c.Limitations, "latest_available_sitemap_batch: "+weeks[0]+"; batch date is not publication date")
	known := s.previousRecords()
	read := 0
	for _, week := range weeks {
		if s.cnSitemapPage(ctx, week, data, &c, known, &read, 0) {
			return
		}
	}
	c.StopReason = "available sitemap candidates scanned; CN jurisdiction coverage remains partial"
}

func (s *service) cnPage(ctx context.Context, address, identity string, data *snapshot, c *coverage) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("CN collection interrupted: %w", err)
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("CN source URL: %w", err)
	}
	rules := s.robotsFor(ctx, u, data)
	if !rules.allows(u) {
		return nil, fmt.Errorf("CN source: %s", rules.limitation(address))
	}
	raw, err := s.cached(ctx, address, identity, data)
	if err == nil {
		c.Pages = append(c.Pages, provenance{URL: address, SHA256: hashBytes(raw)})
	}
	return raw, err
}

func (s *service) cnSitemapPage(ctx context.Context, file string, data *snapshot, c *coverage,
	known map[string]record, read *int, depth int,
) bool {
	address := cnBase + "/sitemap/" + file
	raw, err := s.cnPage(ctx, address, "cn-sitemap:"+file+":"+data.Through, data, c)
	if err != nil {
		c.Cursor, c.StopReason = address, err.Error()
		return true
	}
	children, candidates := cnSitemap(raw, data.From, data.Through, s.config.PatentTerms)
	for _, item := range candidates {
		if s.cnDocument(ctx, item, data, c, known, read) {
			return true
		}
	}
	// Только недельные индексы и их прямые части: произвольные адреса, циклы и вложенность не обходятся.
	if depth == 0 {
		for _, child := range children {
			if strings.HasPrefix(child, strings.TrimSuffix(file, ".html")+"-p") &&
				s.cnSitemapPage(ctx, child, data, c, known, read, depth+1) {
				return true
			}
		}
	}
	return false
}

// cnEnglishTitle: документ отобран по английскому заголовку карты сайта, а DC.title китайский; оригинал остаётся.
func cnEnglishTitle(row *record, title string) {
	if title == "" || row.Title == title {
		return
	}
	if row.OriginalTitle == "" {
		row.OriginalTitle = row.Title
	}
	row.Title, row.TitleBasis = title, "Google Patents sitemap (English title); original in original_title"
}

func (s *service) cnDocument(ctx context.Context, item cnCandidate, data *snapshot, c *coverage,
	known map[string]record, read *int,
) bool {
	id := item.ID
	if previous, ok := known[id]; ok && previous.Source == cnSource {
		if previous.Date >= data.From && previous.Date <= data.Through {
			cnEnglishTitle(&previous, item.Title)
			data.Records = append(data.Records, previous)
		}
		return false
	}
	address, identity := cnBase+"/patent/"+id+"/zh", cnParserVersion+":"+id
	// Закэшированные отказы не должны вечно съедать бюджет новых документов: точка возврата — неизменный сырой кэш.
	cache := filepath.Join(s.dir, "cache", hashBytes([]byte(identity+"\x00"+address)))
	_, cachedErr := os.Stat(cache)
	missing := filepath.Join(s.dir, "cn-missing-"+id+".json")
	if info, err := os.Stat(missing); err == nil && time.Since(info.ModTime()) < 7*24*time.Hour {
		c.Limitations = append(c.Limitations, "publication_unavailable; retry after seven days: "+id)
		return false
	}
	if cachedErr != nil {
		// Исчерпанный бюджет пропускает только незакэшированные документы: прочитанные глубже записи остаются в снимке.
		if *read >= s.config.CNDocuments {
			c.Cursor, c.StopReason = id, "CN document budget reached; uncached documents wait for the next pass"
			return false
		}
		*read++
	}
	body, err := s.cnPage(ctx, address, identity, data, c)
	if failure, ok := errors.AsType[*sourceHTTPError](err); ok &&
		(failure.Status == http.StatusNotFound || failure.Status == http.StatusGone) {
		saveErr := atomicSourceJSON(missing, data.ObservedAt)
		if saveErr == nil {
			c.Limitations = append(c.Limitations, "publication_unavailable; retry after seven days: "+id)
			return false
		}
		err = errors.Join(err, saveErr)
	}
	if err != nil {
		c.Cursor, c.StopReason = id, err.Error()
		return true
	}
	row, err := cnRecord(body, id)
	if err != nil {
		c.Limitations = append(c.Limitations, fmt.Sprintf("publication_parse_failed: %s: %v", id, err))
		return false
	}
	if len(row.Text) > 500<<10 {
		c.Limitations = append(c.Limitations, "publication_text_exceeds_500KiB: "+id)
		return false
	}
	if row.Date >= data.From && row.Date <= data.Through {
		row.ObservedAt, row.AvailableAt = data.ObservedAt, data.ObservedAt
		cnEnglishTitle(&row, item.Title)
		data.Records = append(data.Records, row)
		known[row.ID] = row
	}
	return false
}

// Запросом может стать только фиксированное имя файла или проверенный номер публикации.
func cnSitemap(raw []byte, from, through string, terms []string) ([]string, []cnCandidate) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, nil
	}
	files, rows := []string{}, []cnCandidate{}
	seen := map[string]bool{}
	visit(doc, func(n *html.Node) {
		if n.Data == "a" {
			file := attr(n, "href")
			if parts := cnSitemapFile.FindStringSubmatch(file); len(parts) == 3 && !seen[file] && cnWeekInWindow(parts, from, through) {
				files, seen[file] = append(files, file), true
			}
		}
		if n.Data != "li" || n.FirstChild == nil || n.FirstChild.Type != html.TextNode {
			return
		}
		id, title, ok := strings.Cut(strings.TrimSpace(n.FirstChild.Data), " - ")
		if !ok || !cnID.MatchString(id) || seen[id] {
			return
		}
		title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(title), ":"))
		if !candidatePatent(record{Title: title}, terms) {
			return
		}
		seen[id] = true
		rows = append(rows, cnCandidate{ID: id, Title: title})
	})
	slices.Sort(files)
	slices.Reverse(files)
	return files, rows
}

func cnWeekInWindow(parts []string, from, through string) bool {
	year, _ := strconv.Atoi(parts[1])
	week, _ := strconv.Atoi(parts[2])
	if week < 1 || week > 53 {
		return false
	}
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	monday := jan4.AddDate(0, 0, -(int(jan4.Weekday())+6)%7+(week-1)*7)
	return monday.Format(time.DateOnly) <= through && monday.AddDate(0, 0, 6).Format(time.DateOnly) >= from
}
