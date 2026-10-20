package application

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"strings"
)

type liveJATSText struct {
	Text string `xml:",innerxml"`
}

func liveParsePaperJATS(ctx context.Context, raw []byte, doi string) (liveCardContextDocument, error) {
	if len(raw) > liveCardContextMaxHTML || bytes.Contains(raw, []byte("<!ENTITY")) {
		return liveCardContextDocument{}, errors.New("JATS exceeds bounds or declares entities")
	}
	var article struct {
		XMLName xml.Name `xml:"article"`
		Meta    struct {
			IDs []struct {
				Kind  string `xml:"pub-id-type,attr"`
				Value string `xml:",chardata"`
			} `xml:"article-id"`
			Title    liveJATSText `xml:"title-group>article-title"`
			Abstract liveJATSText `xml:"abstract"`
		} `xml:"front>article-meta"`
		Body liveJATSText `xml:"body"`
	}
	// #nosec G709 -- Объём ограничен, внешние сущности не разрешаются, объявления сущностей отклонены.
	if xml.Unmarshal(raw, &article) != nil {
		return liveCardContextDocument{}, errors.New("invalid JATS article")
	}
	matched := false
	for _, id := range article.Meta.IDs {
		matched = matched || id.Kind == livePaperDOIField && strings.EqualFold(strings.TrimSpace(id.Value), doi)
	}
	if !matched || strings.TrimSpace(article.Body.Text) == "" {
		return liveCardContextDocument{}, errors.New("JATS DOI mismatch or missing body")
	}
	// Разметка остаётся инертной; общий отбор блоков сохраняет результаты, таблицы и ограничения.
	body := strings.NewReplacer("<title>", "<h2>", "<title ", "<h2 ", "</title>", "</h2>",
		"<sec>", "<section>", "<sec ", "<section ", "</sec>", "</section>").Replace(article.Body.Text)
	markup := "<article><h1>" + article.Meta.Title.Text + "</h1><div class=\"ltx_abstract\">" +
		article.Meta.Abstract.Text + "</div>" + body + "</article>"
	parsedBody, err := liveParseCardHTML(ctx, []byte("<article>"+body+"</article>"))
	if err != nil || strings.TrimSpace(parsedBody.text) == "" {
		return liveCardContextDocument{}, errors.New("JATS has no readable body")
	}
	return liveParseCardHTML(ctx, []byte(markup))
}
