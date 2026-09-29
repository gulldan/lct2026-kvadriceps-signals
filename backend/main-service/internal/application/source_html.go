package application

import (
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"io"
	"slices"
	"strings"
)

// SourceHTMLText берёт только инертный текст: скрипты, стили, встроенное и скрытое доказательством
// не считаются, ни ресурсы, ни ссылки не загружаются.
func SourceHTMLText(input io.Reader) (string, error) {
	doc, err := html.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse source HTML: %w", err)
	}
	var out strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if hiddenHTML(n) {
			return
		}
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
			out.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if slices.Contains([]string{"p", "div", "h1", "h2", "h3", "li", "tr", "br", "section"}, n.Data) {
			out.WriteByte('\n')
		}
	}
	visit(doc)
	var lines []string
	for line := range strings.SplitSeq(out.String(), "\n") {
		if normalized := strings.Join(strings.Fields(line), " "); normalized != "" {
			lines = append(lines, normalized)
		}
	}
	text := strings.Join(lines, "\n\n")
	if text == "" {
		return "", errors.New("source HTML has no usable text")
	}
	return text, nil
}

func hiddenHTML(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if slices.Contains([]string{"head", "script", "style", "noscript", "iframe", "object", "svg", "nav"}, n.Data) {
		return true
	}
	for _, attr := range n.Attr {
		if attr.Key == "hidden" || (attr.Key == "aria-hidden" && attr.Val == "true") {
			return true
		}
	}
	return false
}
