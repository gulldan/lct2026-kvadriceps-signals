package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type liveOpenLocation struct {
	URL     string `json:"url"`
	Landing string `json:"url_for_landing_page"`
}

const livePaperDiscussion = "discussion"

func (c *livePaperTextClient) openCopy(ctx context.Context, doi string) (liveCardContextDocument, error) {
	endpoint := c.unpaywall + "/" + url.PathEscape(doi)
	raw, _, err := c.read(ctx, endpoint+"?"+url.Values{"email": {c.email}}.Encode())
	if err != nil {
		return liveCardContextDocument{}, err
	}
	var result struct {
		DOI       string             `json:"doi"`
		Open      bool               `json:"is_oa"`
		Best      *liveOpenLocation  `json:"best_oa_location"`
		Locations []liveOpenLocation `json:"oa_locations"`
	}
	if json.Unmarshal(raw, &result) != nil || !strings.EqualFold(result.DOI, doi) || !result.Open {
		return liveCardContextDocument{}, errors.New("unpaywall has no matching open copy")
	}
	locations := result.Locations
	if result.Best != nil {
		locations = append([]liveOpenLocation{*result.Best}, locations...)
	}
	seen := map[string]bool{}
	for _, location := range locations {
		address := location.Landing
		if address == "" {
			address = location.URL
		}
		u, err := url.Parse(address)
		if err != nil || !livePaperPublicURL(u) || seen[address] {
			continue
		}
		if len(seen) == 2 {
			break
		}
		seen[address] = true
		body, resolved, err := c.read(ctx, address)
		if err != nil || !liveOpenHTMLIdentity(body, doi) {
			continue
		}
		document, err := liveParseCardHTML(ctx, body)
		if err != nil || !liveOpenHTMLBody(document) {
			continue
		}
		document.address, document.rawHash, document.observed = resolved, liveCardTextHash(string(body)), time.Now().UTC()
		// Контакт нужен поставщику, но не должен попадать в ответ, журнал или сохранённое основание.
		document.provider, document.lookupURL, document.lookupHash = "unpaywall", endpoint, liveCardTextHash(string(raw))
		document.parser = "open-html-card-blocks-v1"
		return document, nil
	}
	return liveCardContextDocument{}, errors.New("open copy has no verified readable HTML article; PDF is not parsed")
}

// Страница аннотации с верным DOI ещё не является телом статьи.
func liveOpenHTMLBody(document liveCardContextDocument) bool {
	sections := map[string]bool{}
	for _, block := range document.blocks {
		if !strings.HasPrefix(block.kind, "h") {
			continue
		}
		heading := strings.ToLower(block.text)
		for _, name := range []string{"method", "result", "evaluation", livePaperDiscussion, "conclusion", "limitation"} {
			if strings.Contains(heading, name) {
				sections[name] = true
			}
		}
	}
	return len(sections) >= 2 && utf8.RuneCountInString(document.text) >= 1000
}

// Адрес из каталога ещё не подтверждает содержимое: издательская страница обязана назвать тот же DOI.
func liveOpenHTMLIdentity(raw []byte, doi string) bool {
	if bytes.HasPrefix(raw, []byte("%PDF-")) {
		return false
	}
	tokens := html.NewTokenizer(bytes.NewReader(raw))
	for range liveCardContextMaxNodes {
		token := tokens.Next()
		if token == html.ErrorToken {
			return false
		}
		if token != html.StartTagToken && token != html.SelfClosingTagToken {
			continue
		}
		tag := tokens.Token()
		if tag.Data != "meta" {
			continue
		}
		name, content := "", ""
		for _, attr := range tag.Attr {
			switch attr.Key {
			case keyName:
				name = strings.ToLower(attr.Val)
			case "content":
				content = strings.TrimSpace(attr.Val)
			}
		}
		if name == "citation_doi" && strings.EqualFold(strings.TrimPrefix(content, "https://doi.org/"), doi) {
			return true
		}
	}
	return false
}
