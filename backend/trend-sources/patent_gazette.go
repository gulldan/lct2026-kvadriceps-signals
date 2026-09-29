package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const gazette = "https://patentsgazette.uspto.gov"
const kindWordPress = "wordpress"
const kindPatents = "patents"
const textNotCollected = "not_collected"
const fieldDatePublished = "datePublished"
const articleText = "article_text"
const schemeHTTPS = "https"
const kindURL = "url"
const kindURLs = "urls"
const kindSitemap = "sitemap"

var issueLink = regexp.MustCompile(`^https://patentsgazette\.uspto\.gov/week[0-9]{2}$`)
var patentLink = regexp.MustCompile(`^\.\./html/[0-9-]+/(US[0-9A-Z]+)-([0-9]{8})\.html$`)

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func content(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(content(c))
	}
	return b.String()
}
func visit(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visit(c, fn)
	}
}

type issue struct{ URL, Date string }

func issues(body []byte, from, through string) ([]issue, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse gazette root: %w", err)
	}
	found := []issue{}
	visit(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" || !issueLink.MatchString(attr(n, "href")) {
			return
		}
		date, err := time.Parse("January 02, 2006", strings.TrimSpace(content(n)))
		if err != nil {
			return
		}
		day := date.Format(time.DateOnly)
		if day >= from && day <= through {
			found = append(found, issue{attr(n, "href"), day})
		}
	})
	if len(found) == 0 {
		return nil, errors.New("no published gazette issue in the current month yet")
	}
	return found, nil
}

// patentIndexItem: именованный якорь отмечает владельца названия; без него текст — двусмысленная ссылка.
func patentIndexItem(n *html.Node, source issue) []record {
	rows := []record{}
	var title strings.Builder
	named := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			title.WriteString(c.Data)
			continue
		}
		if c.Data != "a" {
			title.WriteString(content(c))
			continue
		}
		if attr(c, "name") != "" {
			named = true
		}
		match := patentLink.FindStringSubmatch(attr(c, "href"))
		if match == nil {
			continue
		}
		day, err := time.Parse("20060102", match[2])
		if err != nil || day.Format(time.DateOnly) != source.Date {
			continue
		}
		name := strings.Join(strings.Fields(title.String()), " ")
		if name == "" || len(name) > 2000 || strings.Contains(name, "See--") {
			continue
		}
		citation := ""
		if !named {
			citation, name = name, ""
		}
		rows = append(rows, record{ID: match[1], Title: name, Citation: citation, Date: source.Date,
			DateBasis: "USPTO issue date", TitleBasis: "unambiguous patentee index", TextStatus: metadataOnlyText,
			URL: source.URL + "/OG/" + strings.TrimPrefix(match[0], "../"), Source: patentSource})
	}
	return rows
}

func patentRows(body []byte, source issue) ([]record, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse gazette index: %w", err)
	}
	rows := []record{}
	linked := false
	visit(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "a" && patentLink.MatchString(attr(n, "href")) {
			linked = true
		}
		if n.Data == elementDiv && attr(n, "class") == "list_item1" {
			rows = append(rows, patentIndexItem(n, source)...)
		}
	})
	if !bytes.Contains(body, []byte("LIST OF PATENTEES")) {
		return nil, errors.New("unexpected gazette index page")
	}
	if len(rows) == 0 && linked {
		return nil, errors.New("gazette has patent links but no rows matching the verified issue date")
	}
	return rows, nil
}
