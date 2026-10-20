package application

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Фонд содействия инновациям публикует итоги конкурсов страницами сайта, а перечень победителей — DOCX-файлом
// без названий компаний: только номер заявки, проект и сумма. robots.txt фонда просит Crawl-delay 2.

const (
	liveMoneyFSIURL     = "https://fasie.ru/competitions/"
	liveMoneyFSISite    = "https://fasie.ru"
	liveMoneyFSISource  = "Фонд содействия инновациям: итоги конкурсов"
	liveMoneyFSILicense = "открытые данные фонда, лицензия не указана"
	liveMoneyFSIPages   = 20
	liveMoneyFSIDelay   = 2 * time.Second
	liveMoneyFSIRead    = 8 << 20
)

type liveMoneyFSIPage struct {
	URL, Title, Date string
}

type liveMoneyFSIPatterns struct {
	item, article, tag, summary, program, quoted, document, amount *regexp.Regexp
}

func liveMoneyFSIRules() liveMoneyFSIPatterns {
	return liveMoneyFSIPatterns{
		item: regexp.MustCompile(`(?s)<a href="(/press/fund/[^"]+-results/)">\s*([^<]*?)\s*</a>` +
			`.*?<b>(\d{2})\.(\d{2})\.(\d{4})</b>`),
		article:  regexp.MustCompile(`(?s)itemprop="text">(.*?)</div>`),
		tag:      regexp.MustCompile(`<[^>]+>`),
		summary:  regexp.MustCompile(`[^.]*рекомендован[^.]*`),
		program:  regexp.MustCompile(`конкурса «([^»]+)»`),
		quoted:   regexp.MustCompile(`«([^»]+)»`),
		document: regexp.MustCompile(`href="([^"]+\.docx)"`),
		amount:   regexp.MustCompile(`на сумму ([\d,.]+) млн`),
	}
}

func liveMoneyFSI(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	rules := liveMoneyFSIRules()
	listing, err := liveMoneyFSIFetch(ctx, client, liveMoneyFSIURL)
	if err != nil {
		return nil, 0, fmt.Errorf("фси: список итогов: %w", err)
	}
	pages := liveMoneyFSIListing(rules, listing)
	if len(pages) == 0 {
		return nil, 0, errors.New("фси: разметка списка итогов не распознана")
	}
	events, records := []LiveMoneyEvent{}, 0
	for _, page := range pages {
		if page.Date < from || page.Date > through {
			continue
		}
		if records == liveMoneyFSIPages {
			return events, records, fmt.Errorf("фси: итогов больше %d; остаток не прочитан", liveMoneyFSIPages)
		}
		records++
		detail, err := liveMoneyFSIFetch(ctx, client, page.URL)
		if err != nil {
			return events, records, fmt.Errorf("фси: %s: %w", page.URL, err)
		}
		var document []byte
		if m := rules.document.FindSubmatch(detail); m != nil {
			// Недоступный перечень — не отказ источника: конкурс остаётся одним событием с суммой из анонса.
			document, _ = liveMoneyFSIDocument(ctx, client, string(m[1]))
		}
		events = append(events, liveMoneyFSIEvents(rules, rubrics, page, detail, document)...)
	}
	return events, records, nil
}

// liveMoneyFSIFetch выдерживает Crawl-delay перед каждым запросом, включая первый.
func liveMoneyFSIFetch(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	if err := liveSleep(ctx, liveMoneyFSIDelay); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(address, "http") {
		address = liveMoneyFSISite + address
	}
	return liveMarketFetch(ctx, client, http.MethodGet, address, nil, nil, liveMoneyFSIRead)
}

func liveMoneyFSIListing(rules liveMoneyFSIPatterns, listing []byte) []liveMoneyFSIPage {
	matches := rules.item.FindAllSubmatch(listing, -1)
	pages := make([]liveMoneyFSIPage, 0, len(matches))
	for _, m := range matches {
		pages = append(pages, liveMoneyFSIPage{URL: liveMoneyFSISite + string(m[1]),
			Title: html.UnescapeString(strings.Join(strings.Fields(string(m[2])), " ")),
			Date:  string(m[5]) + "-" + string(m[4]) + "-" + string(m[3])})
	}
	return pages
}

func liveMoneyFSIDocument(ctx context.Context, client *http.Client, href string) ([]byte, error) {
	raw, err := liveMoneyFSIFetch(ctx, client, href)
	if err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("фси: docx: %w", err)
	}
	file, err := archive.Open("word/document.xml")
	if err != nil {
		return nil, fmt.Errorf("фси: docx: %w", err)
	}
	defer func() { _ = file.Close() }()
	document, err := io.ReadAll(io.LimitReader(file, liveMoneyFSIRead))
	if err != nil {
		return nil, fmt.Errorf("фси: docx: %w", err)
	}
	return document, nil
}

// liveMoneyFSIEvents даёт событие на каждую строку перечня победителей; без перечня — одно на конкурс, честно
// без компании. Заголовок у всех строк общий, поэтому проект идёт в цитату: иначе события неразличимы.
func liveMoneyFSIEvents(rules liveMoneyFSIPatterns, rubrics []liveRubric, page liveMoneyFSIPage,
	detail, document []byte) []LiveMoneyEvent {
	article := detail
	if m := rules.article.FindSubmatch(detail); m != nil {
		article = m[1]
	}
	plain := html.UnescapeString(strings.Join(strings.Fields(rules.tag.ReplaceAllString(string(article), " ")), " "))
	// Имя конкурса в анонсе написано как положено («Развитие-Био»), в заголовке — капсом; без кавычек остаётся фонд.
	m := rules.program.FindStringSubmatch(plain)
	if m == nil {
		m = rules.quoted.FindStringSubmatch(page.Title)
	}
	program, investors := "", "ФСИ"
	if m != nil {
		program, investors = m[1], "ФСИ, "+m[1]
	}
	event := func(project, amount, quote string) LiveMoneyEvent {
		e := liveMoneyGrant(rubrics, liveMoneyFSISource, page.Title, strings.Trim(project+". "+program, ". "))
		e.Region, e.Country, e.Date, e.URL = liveMoneyRegionCIS, "RU", page.Date, page.URL
		e.License, e.Investors, e.Amount, e.Quote = liveMoneyFSILicense, investors, amount, firstRunes(quote, 200)
		return e
	}
	rows := liveMoneyFSIRows(document)
	if len(rows) < 2 {
		e := event("", "", strings.TrimSpace(rules.summary.FindString(plain)))
		if m := rules.amount.FindStringSubmatch(plain); m != nil {
			e.Amount = m[1] + " млн ₽"
		}
		e.ID = liveMoneyID("fasie", page.URL)
		return []LiveMoneyEvent{e}
	}
	events := []LiveMoneyEvent{}
	for i, row := range rows[1:] {
		name := liveMoneyFSICell(rows[0], row, "проект")
		if name == "" {
			continue
		}
		e := event(name, liveMoneyRubles(liveMoneyFSICell(rows[0], row, "руб", "грант")), name)
		e.Company = liveMoneyFSICell(rows[0], row, "предприяти", "организац", "компани", "заявител")
		e.ID = liveMoneyID("fasie", page.URL, cmp.Or(liveMoneyFSICell(rows[0], row, "заявки"), strconv.Itoa(i)))
		events = append(events, e)
	}
	return events
}

// liveMoneyFSICell ищет столбец по слову заголовка, а не по номеру: перечни разных программ оформлены по-разному.
func liveMoneyFSICell(header, row []string, keys ...string) string {
	for i, head := range header {
		for _, key := range keys {
			if strings.Contains(strings.ToLower(head), key) && i < len(row) {
				return row[i]
			}
		}
	}
	return ""
}

// liveMoneyFSIRows читает только первую таблицу: вторая в перечнях фонда — резерв «при появлении средств».
func liveMoneyFSIRows(document []byte) [][]string {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	var rows [][]string
	var row []string
	var cell strings.Builder
	inCell := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return rows
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "tc" {
				cell.Reset()
				inCell = true
			}
		case xml.CharData:
			if inCell {
				cell.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "tc":
				row = append(row, strings.Join(strings.Fields(cell.String()), " "))
				inCell = false
			case "tr":
				rows = append(rows, row)
				row = nil
			case "tbl":
				return rows
			}
		}
	}
}

func liveMoneyRubles(raw string) string {
	value, err := strconv.ParseFloat(strings.NewReplacer(" ", "", " ", "", ",", ".").Replace(raw), 64)
	if err != nil || value <= 0 {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSuffix(strconv.FormatFloat(value/1e6, 'f', 1, 64), ".0"), ".", ",") + " млн ₽"
}
