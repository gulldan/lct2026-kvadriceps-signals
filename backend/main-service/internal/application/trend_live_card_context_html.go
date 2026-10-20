package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	liveCardContextMaxText  = 256 << 10
	liveCardContextMaxNodes = 50000
	liveCardTableBlock      = "table"
)

type liveCardContextDocument struct {
	arxivID, address, rawHash               string
	provider, lookupURL, lookupHash, parser string
	observed                                time.Time
	text                                    string
	lead                                    string
	blocks                                  []liveCardContextBlock
}

type liveCardContextBlock struct {
	text     string
	span     LiveCardContextRange
	kind     string
	headings []int
}

type liveCardHTMLBlocks struct {
	text          string
	cursor, runes int
	blocks        []liveCardContextBlock
	headings      []int
}

func liveParseCardHTML(ctx context.Context, raw []byte) (liveCardContextDocument, error) {
	if len(raw) > liveCardContextMaxHTML || !utf8.Valid(raw) {
		return liveCardContextDocument{}, errors.New("arxiv context HTML exceeds bounds")
	}
	if err := liveCardHTMLBounds(ctx, raw); err != nil {
		return liveCardContextDocument{}, err
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return liveCardContextDocument{}, fmt.Errorf("parse arxiv context: %w", err)
	}
	article, err := liveCardHTMLArticle(ctx, doc)
	if err != nil {
		return liveCardContextDocument{}, err
	}
	liveCardTableWhitespace(article)
	liveCardDropTeX(article)
	text, err := liveCardHTMLText(article)
	if err != nil {
		return liveCardContextDocument{}, err
	}
	if len(text) > liveCardContextMaxText {
		return liveCardContextDocument{}, errors.New("arxiv context body exceeds text limit")
	}
	collector := liveCardHTMLBlocks{text: text}
	if err := collector.visit(ctx, article); err != nil {
		return liveCardContextDocument{}, err
	}
	collector.gap(len(text))
	return liveCardContextDocument{text: text, lead: liveCardHTMLLead(article), blocks: collector.blocks}, nil
}

// Каталог может хранить аннотацию прежней версии: вводный текст берётся из того же HTML, что и результаты.
func liveCardHTMLLead(article *html.Node) string {
	title, abstract := "", ""
	for node := range article.Descendants() {
		if node.Type != html.ElementNode {
			continue
		}
		if node.Data == "h1" && title == "" {
			title, _ = liveCardHTMLText(node)
		}
		for _, attr := range node.Attr {
			if attr.Key == "class" && slices.Contains(strings.Fields(attr.Val), "ltx_abstract") {
				abstract, _ = liveCardHTMLText(node)
			}
		}
	}
	return strings.TrimSpace(title + "\n\n" + abstract)
}

// Токены считаются до создания дерева: маленькие теги не раздуют ограниченный HTML в миллионы узлов.
func liveCardHTMLBounds(ctx context.Context, raw []byte) error {
	tokens := html.NewTokenizer(bytes.NewReader(raw))
	for count := 0; count <= liveCardContextMaxNodes; count++ {
		if ctx.Err() != nil {
			return fmt.Errorf("parse arxiv context: %w", ctx.Err())
		}
		if tokens.Next() == html.ErrorToken {
			if errors.Is(tokens.Err(), io.EOF) {
				return nil
			}
			return fmt.Errorf("tokenize arxiv context: %w", tokens.Err())
		}
	}
	return errors.New("arxiv HTML exceeds token limit")
}

// Отступы HTML между ячейками не являются границами строк таблицы: границы задаёт сам tr.
func liveCardTableWhitespace(article *html.Node) {
	for node := range article.Descendants() {
		if node.Type != html.ElementNode || node.Data != liveCardTableBlock {
			continue
		}
		for cell := range node.Descendants() {
			if cell.Type == html.TextNode {
				cell.Data = strings.Join(strings.Fields(cell.Data), " ")
			}
		}
	}
}

// LaTeXML повторяет каждую формулу исходником TeX: в цитате выходило «× 2 \times 2» и «rank 128 128», и
// дословный факт проверяющего не находился в ней (замер 27.09 — 3 из 21 отказа). Остаётся видимая запись.
func liveCardDropTeX(article *html.Node) {
	var duplicates []*html.Node
	for node := range article.Descendants() {
		if node.Type == html.ElementNode && (node.Data == "annotation" || node.Data == "annotation-xml") {
			duplicates = append(duplicates, node)
		}
	}
	for _, node := range duplicates {
		node.Parent.RemoveChild(node)
	}
}

func liveCardHTMLArticle(ctx context.Context, root *html.Node) (*html.Node, error) {
	stack := []*html.Node{root}
	var article *html.Node
	count := 0
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if count > liveCardContextMaxNodes || ctx.Err() != nil {
			return nil, errors.New("arxiv context tree exceeds budget or search cancelled")
		}
		if node.Type == html.ElementNode && node.Data == "article" {
			if article != nil {
				return nil, errors.New("arxiv context has multiple article bodies")
			}
			article = node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			stack = append(stack, child)
		}
	}
	if article == nil {
		return nil, errors.New("arxiv HTML has no article body")
	}
	return article, nil
}

func liveCardHTMLText(node *html.Node) (string, error) {
	var raw bytes.Buffer
	if err := html.Render(&raw, node); err != nil {
		return "", fmt.Errorf("render inert arxiv block: %w", err)
	}
	return SourceHTMLText(&raw)
}

func (c *liveCardHTMLBlocks) visit(ctx context.Context, node *html.Node) error {
	if ctx.Err() != nil {
		return fmt.Errorf("read arxiv blocks: %w", ctx.Err())
	}
	if hiddenHTML(node) {
		return nil
	}
	if kind := liveCardBlockKind(node); kind != "" {
		text, _ := liveCardHTMLText(node)
		if text == "" {
			// Пустой рисунок без подписи не является текстовым свидетельством.
			return nil
		}
		return c.add(text, kind)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := c.visit(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

func liveCardBlockKind(node *html.Node) string {
	if node.Type != html.ElementNode {
		return ""
	}
	switch node.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", liveCardTableBlock, "ul", "ol", "dl", "pre", "blockquote":
		return node.Data
	case "figure":
		for child := range node.Descendants() {
			if child.Type == html.ElementNode && child.Data == liveCardTableBlock {
				return liveCardTableBlock
			}
		}
		return "figure"
	default:
		return ""
	}
}

func (c *liveCardHTMLBlocks) add(text, kind string) error {
	offset := strings.Index(c.text[c.cursor:], text)
	if offset < 0 {
		return errors.New("arxiv block cannot be located in the normalized body")
	}
	c.gap(c.cursor + offset)
	start := c.runes
	c.runes += utf8.RuneCountInString(text)
	c.cursor += len(text)
	if strings.HasPrefix(kind, "h") {
		c.headings = slices.DeleteFunc(c.headings, func(index int) bool { return c.blocks[index].kind >= kind })
	}
	c.blocks = append(c.blocks, liveCardContextBlock{text: text, kind: kind,
		span: LiveCardContextRange{StartRune: start, EndRune: c.runes}, headings: slices.Clone(c.headings)})
	if strings.HasPrefix(kind, "h") {
		c.headings = append(c.headings, len(c.blocks)-1)
	}
	return nil
}

func (c *liveCardHTMLBlocks) gap(through int) {
	text := c.text[c.cursor:through]
	if strings.TrimSpace(text) != "" {
		c.blocks = append(c.blocks, liveCardContextBlock{text: text, kind: "text",
			span:     LiveCardContextRange{StartRune: c.runes, EndRune: c.runes + utf8.RuneCountInString(text)},
			headings: slices.Clone(c.headings)})
	}
	c.runes += utf8.RuneCountInString(text)
	c.cursor = through
}
