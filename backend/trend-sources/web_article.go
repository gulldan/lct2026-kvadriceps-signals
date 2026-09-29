package main

import (
	"bytes"
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	trafilatura "github.com/markusmobius/go-trafilatura/v2"
	"golang.org/x/net/html"
)

const webArticleParserVersion = "go-trafilatura/v2@v2.2.1+publisher-v2"

var errArticleBodyUnavailable = errors.New("article body unavailable")

type articleFields struct {
	title, day, published, canonical, original, jsonBody string
	language                                             string
	conflictingDates                                     bool
	body                                                 *html.Node
}

func (f *articleFields) publication(value string) {
	day := sourceDay(value)
	if day == "" {
		return
	}
	f.conflictingDates = f.conflictingDates || f.day != "" && f.day != day
	f.day = day
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		f.published = at.UTC().Format(time.RFC3339)
	}
}

func (f *articleFields) node(n *html.Node, base string) {
	if n.Type != html.ElementNode {
		return
	}
	switch n.Data {
	case "html":
		f.language = strings.TrimSpace(attr(n, "lang"))
	case elementTitle:
		f.title = cmp.Or(f.title, strings.Join(strings.Fields(content(n)), " "))
	case "meta":
		key := strings.ToLower(cmp.Or(attr(n, "property"), attr(n, "name")))
		switch key {
		case "article:published_time", "datepublished", "date", "citation_publication_date":
			f.publication(attr(n, "content"))
		case "og:title":
			f.title = attr(n, "content")
		}
	case "link":
		if slices.Contains(strings.Fields(attr(n, "rel")), "canonical") {
			f.canonical = absolutePublic(attr(n, "href"), base)
		}
	case "script":
		if attr(n, "type") == "application/ld+json" {
			var value any
			if json.Unmarshal([]byte(content(n)), &value) == nil {
				f.linkedData(value, base)
			}
		}
	}
	if attr(n, "itemprop") == fieldDatePublished {
		f.publication(cmp.Or(attr(n, "content"), attr(n, "datetime")))
	}
	// Явное тело издателя — запасной путь для коротких заметок ниже порога экстрактора; узел исходный ради якорей.
	if attr(n, "itemprop") == "articleBody" || strings.Contains(attr(n, "class"), "field--name-body") ||
		n.Data == "article" && f.body == nil {
		f.body = n
	}
}

// Факты публикации — только из объекта статьи: рекурсивный обход JSON-LD цепляет даты рекомендаций и комментариев.
func (f *articleFields) linkedData(value any, base string) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			f.linkedData(item, base)
		}
	case map[string]any:
		for _, key := range []string{"@graph", "mainEntity"} {
			f.linkedData(v[key], base)
		}
		kind, _ := v["@type"].(string)
		if !slices.Contains([]string{"Article", "NewsArticle", "BlogPosting", "ScholarlyArticle", "TechArticle", "Report"}, kind) {
			return
		}
		if address, ok := v["url"].(string); ok && absolutePublic(address, base) != absolutePublic(base, base) &&
			absolutePublic(address, base) != f.canonical {
			return
		}
		if date, ok := v[fieldDatePublished].(string); ok {
			f.publication(date)
		}
		f.jsonBody, _ = v["articleBody"].(string)
		if original, ok := v["isBasedOn"].(string); ok {
			f.original = absolutePublic(original, base)
		}
	}
}

func articleDocument(raw []byte, row record, observed string) (record, error) {
	if len(raw) > 8<<20 {
		return row, errors.New("article page exceeds 8 MiB")
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return row, fmt.Errorf("parse article: %w", err)
	}
	var fields articleFields
	nodes := 0
	visit(doc, func(n *html.Node) {
		nodes++
		fields.node(n, row.URL)
	})
	if nodes > 50000 {
		return row, errors.New("article exceeds 50000 HTML nodes")
	}
	if fields.conflictingDates || fields.day != "" && row.Date != "" && fields.day != row.Date {
		return row, errors.New("feed/article publication dates disagree")
	}
	text, anchors, method := articleBody(doc, fields, row.URL)
	if utf8.RuneCountInString(text) < 80 {
		return row, errArticleBodyUnavailable
	}
	if len(text) > 256<<10 {
		return row, errors.New("article body exceeds 256 KiB")
	}
	row.Text, row.Anchors = text, anchors
	row.Language = cmp.Or(row.Language, fields.language)
	row.TextStatus, row.ParserVersion = articleText, method
	row.ContentHash = hashBytes([]byte(text))
	row.Title = cmp.Or(row.Title, fields.title, row.URL)
	row.TitleBasis = "publisher record"
	row.ObservedAt, row.AvailableAt = observed, observed
	if fields.day != "" {
		row.Date, row.DateBasis = fields.day, "publisher article datePublished"
		row.PublishedAt = cmp.Or(row.PublishedAt, fields.published)
	}
	if row.Date == "" {
		row.Limitations = appendLimitation(row.Limitations, "publication_date_unknown; excluded from dated trend counts")
	}
	row.CanonicalURL = fields.canonical
	if fields.original != "" {
		row.OriginalURL, row.OriginalBasis = fields.original, "publisher JSON-LD isBasedOn"
	}
	return row, nil
}

// Извлечение офлайн: страницы качает только наш ограниченный клиент. Главное содержимое выбирает библиотека,
// а дата публикации — только из явных полей издателя, не из окружающего текста.
func articleBody(doc *html.Node, fields articleFields, address string) (string, []blockAnchor, string) {
	base, _ := url.Parse(address)
	result, err := trafilatura.ExtractDocument(doc, trafilatura.Options{
		OriginalURL: base, ExcludeComments: true, EnableFallback: true,
		HtmlDateMode: trafilatura.Disabled, MaxTreeSize: 100000,
	})
	if err != nil || result == nil || result.ContentNode == nil {
		return publisherBody(fields)
	}
	text, _ := textNodeAnchored(result.ContentNode)
	if utf8.RuneCountInString(text) < 80 {
		return publisherBody(fields)
	}
	if fields.body != nil {
		original, anchors := textNodeAnchored(fields.body)
		// Часть запасных путей склеивает абзацы без разделителя: при тех же символах сохраняем исходную структуру.
		if strings.Join(strings.Fields(original), "") == strings.Join(strings.Fields(text), "") {
			return original, anchors, webArticleParserVersion
		}
	}
	return text, originalAnchors(doc, text), webArticleParserVersion
}

func publisherBody(fields articleFields) (string, []blockAnchor, string) {
	if fields.body != nil {
		text, anchors := textNodeAnchored(fields.body)
		if utf8.RuneCountInString(text) >= 80 {
			return text, anchors, "publisher-body-v2"
		}
	}
	// У текста JSON-LD нет элемента HTML: локатор на синтетическом DOM не выдумываем, исходник — в сырых данных.
	return textHTML(fields.jsonBody), nil, "publisher-jsonld-v2"
}

// originalAnchors ищет блок в исходной странице: экстрактор переписывает DOM; неоднозначный блок — без локатора.
func originalAnchors(doc *html.Node, text string) []blockAnchor {
	original, locations := textNodeAnchored(doc)
	byText := map[string][]blockAnchor{}
	runes := []rune(original)
	for _, a := range locations {
		block := string(runes[a.Start:a.End])
		byText[block] = append(byText[block], a)
	}
	var anchors []blockAnchor
	at := 0
	for block := range strings.SplitSeq(text, "\n") {
		end := at + utf8.RuneCountInString(block)
		if matches := byText[block]; len(matches) == 1 {
			anchors = append(anchors, blockAnchor{Path: matches[0].Path, Start: at, End: end})
		}
		at = end + 1
	}
	return anchors
}
