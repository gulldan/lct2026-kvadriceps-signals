package main

import (
	"cmp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const elementDiv = "div"
const elementTitle = "title"

// blockElement: повторяющийся баннер узнаётся как шаблон, только пока он отдельный блок.
func blockElement(name string) bool {
	switch name {
	case "p", elementDiv, "li", "ul", "ol", "section", "article", "br", "tr", "table",
		"h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "figcaption", "dd", "dt", "pre", "aside", "main":
		return true
	}
	return false
}

// blockAnchor: смещения — кодовые точки извлечённого текста.
type blockAnchor struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// elementPath — путь вида html/body/article/p[3], по которому узел находится в сохранённой странице.
func elementPath(n *html.Node) string {
	parts := []string{}
	for node := n; node != nil && node.Type == html.ElementNode; node = node.Parent {
		index := 1
		for sibling := node.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
			if sibling.Type == html.ElementNode && sibling.Data == node.Data {
				index++
			}
		}
		part := node.Data
		if index > 1 {
			part += "[" + strconv.Itoa(index) + "]"
		}
		parts = append([]string{part}, parts...)
	}
	return strings.Join(parts, "/")
}

type textPiece struct {
	text string
	node *html.Node
}

func htmlPieces(doc *html.Node) []textPiece {
	pieces := []textPiece{}
	var current strings.Builder
	var owner *html.Node
	flush := func() {
		if strings.TrimSpace(current.String()) != "" {
			pieces = append(pieces, textPiece{current.String(), owner})
		}
		current.Reset()
		owner = nil
	}
	var walk func(*html.Node, *html.Node)
	walk = func(n, container *html.Node) {
		if n.Type == html.ElementNode && slices.Contains(skippedElements(), n.Data) {
			return
		}
		block := n.Type == html.ElementNode && blockElement(n.Data)
		if block {
			flush()
			container = n
		}
		if n.Type == html.TextNode {
			current.WriteString(n.Data)
			if strings.TrimSpace(n.Data) != "" {
				owner = cmp.Or(owner, container, n.Parent)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, container)
		}
		if block {
			flush()
		}
	}
	walk(doc, nil)
	flush()
	return pieces
}

func textHTMLAnchored(raw string) (string, []blockAnchor) {
	doc, e := html.Parse(strings.NewReader(raw))
	if e != nil {
		return "", nil
	}
	return textNodeAnchored(doc)
}

func textNodeAnchored(doc *html.Node) (string, []blockAnchor) {
	blocks := []string{}
	anchors := []blockAnchor{}
	at := 0
	for _, piece := range htmlPieces(doc) {
		// Пробелы HTML переносят абзац, а не начинают новый блок свидетельства.
		lines := []string{piece.text}
		if piece.node != nil && piece.node.Data == "pre" {
			lines = strings.Split(piece.text, "\n")
		}
		for _, line := range lines {
			line = strings.Join(strings.Fields(line), " ")
			if line == "" {
				continue
			}
			if at > 0 {
				at++
			}
			blocks = append(blocks, line)
			// Один якорь на блок в порядке блоков; неизвестный путь остаётся пустым и отбрасывается, а не угадывается.
			path := ""
			if piece.node != nil {
				path = elementPath(piece.node)
			}
			anchors = append(anchors, blockAnchor{path, at, at + utf8.RuneCountInString(line)})
			at += utf8.RuneCountInString(line)
		}
	}
	return strings.Join(blocks, "\n"), anchors
}

func skippedElements() []string {
	return []string{"script", "style", "nav", "footer", "header", "form", "noscript", "template", "svg"}
}

func textHTML(raw string) string { text, _ := textHTMLAnchored(raw); return text }
func sourceDay(s string) string {
	// Календарный день публикации не зависит от момента в UTC для расписания и наблюдений.
	for _, layout := range []string{time.RFC3339, time.DateOnly, "2006-01-02T15:04:05", "2006/01/02",
		time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
		time.RFC822Z, time.RFC822} {
		if at, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return at.Format(time.DateOnly)
		}
	}
	return ""
}
func absolutePublic(raw, base string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, e := url.Parse(raw)
	b, be := url.Parse(base)
	if e != nil || be != nil {
		return ""
	}
	u = b.ResolveReference(u)
	if u.Scheme != schemeHTTPS || u.Host == "" || u.User != nil {
		return ""
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") || k == "fbclid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
