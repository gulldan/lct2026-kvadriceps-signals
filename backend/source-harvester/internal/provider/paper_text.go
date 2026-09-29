package provider

import (
	"encoding/json/v2"
	"strings"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
	"golang.org/x/net/html"
)

// Ревизию задают факты публикации и свидетельство; цитирования и даты обновления индекса — наблюдения, а не новый файл.
func paperRevision(text string, publication *sourcedoc.Date) string {
	raw, _ := json.Marshal(struct {
		Text        string
		Publication *sourcedoc.Date
	}{text, publication})
	return "paper-text-v3:" + digest(raw)
}

// Реферат — единственный полученный раздел, а не вся статья; смещения — кодовые точки сохранённых названия и реферата.
func paperText(title, abstract string) (string, string, []sourcedoc.Section) {
	if abstract == "" {
		return title, "title_only", nil
	}
	text := title + "\n\n" + abstract
	return text, "abstract_only", []sourcedoc.Section{{
		Name: sourcedoc.SectionAbstract, Start: utf8.RuneCountInString(title) + 2, End: utf8.RuneCountInString(text),
	}}
}

// Crossref отдаёт фрагменты JATS с разметкой внутри слов: сущности декодируются один раз, пробелы — на границах блоков.
func stripJATS(s string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimPrefix(strings.Join(strings.Fields(b.String()), " "), "Abstract ")
		case html.TextToken:
			b.Write(z.Text())
		case html.CommentToken, html.DoctypeToken:
			continue
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			local := string(name)
			if _, suffix, ok := strings.Cut(local, ":"); ok {
				local = suffix
			}
			switch local {
			case "p", "sec", "section", "title", "label", "br", "div", "list-item", "h1", "h2", "h3", "h4", "h5", "h6",
				"tr", "td", "th", "table-wrap", "fig", "caption", "ref":
				b.WriteByte(' ')
			}
		}
	}
}
