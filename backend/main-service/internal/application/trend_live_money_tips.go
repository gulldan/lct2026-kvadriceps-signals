package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TIPS — государственная программа Кореи: частный оператор отбирает стартап, государство добавляет грант.
// Реестр открыт без ключа и отдаёт только год отбора, зато фильтрует по нему: читаются лишь годы окна.

const (
	liveMoneyTIPSURL     = "https://jointips.or.kr/api/cms/public/startup/list"
	liveMoneyTIPSCard    = "https://jointips.or.kr/network/startups/detail?companyId="
	liveMoneyTIPSSource  = "TIPS: отбор программы Кореи"
	liveMoneyTIPSLicense = "открытый API программы, условия не заявлены"
	liveMoneyTIPSSize    = 100
	liveMoneyTIPSDelay   = time.Second
)

type liveMoneyTIPSPage struct {
	Total int `json:"total"`
	Data  []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Industry string `json:"industry12Codes"`
		Standard string `json:"industryStdCodes"`
		Intro    string `json:"intro"`
		SelYear  string `json:"selYear"`
		Operator string `json:"operatorName"`
	} `json:"data"`
}

func liveMoneyTIPS(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	events, records := []LiveMoneyEvent{}, 0
	first, _ := strconv.Atoi(from[:min(len(from), 4)])
	last, _ := strconv.Atoi(through[:min(len(through), 4)])
	for year := first; year <= last; year++ {
		// liveMarketFetch повторяет запрос после обрыва: сервер в Корее через прокси срывал TLS на каждой десятой странице.
		for page := 1; ; page++ {
			address := liveMoneyTIPSURL + "?page=" + strconv.Itoa(page) + "&size=" + strconv.Itoa(liveMoneyTIPSSize) +
				"&year=" + strconv.Itoa(year)
			raw, err := liveMarketFetch(ctx, client, http.MethodGet, address, nil, nil, liveMoneyReadLimit)
			if err != nil {
				return events, records, fmt.Errorf("tips: %d, страница %d: %w", year, page, err)
			}
			var result liveMoneyTIPSPage
			if err = json.Unmarshal(raw, &result); err != nil {
				return events, records, fmt.Errorf("tips: %d, страница %d: %w", year, page, err)
			}
			records += len(result.Data)
			events = append(events, liveMoneyTIPSEvents(rubrics, result, from, through)...)
			if len(result.Data) < liveMoneyTIPSSize {
				break
			}
			if err := liveSleep(ctx, liveMoneyTIPSDelay); err != nil {
				return events, records, err
			}
		}
	}
	return events, records, nil
}

// liveMoneyTIPSEvents датирует событие 1 января года отбора и держит окно по году: даты отбора в реестре нет.
// Руководитель (ceo) не сохраняется — слою нужна компания.
func liveMoneyTIPSEvents(rubrics []liveRubric, page liveMoneyTIPSPage, from, through string) []LiveMoneyEvent {
	events := []LiveMoneyEvent{}
	for _, row := range page.Data {
		if row.SelYear < from[:min(len(from), 4)] || row.SelYear > through[:min(len(through), 4)] {
			continue
		}
		intro := strings.Join(strings.Fields(row.Intro), " ")
		quote, _, _ := strings.Cut(intro, ". ")
		industry := liveMoneyTIPSIndustry(row.Industry) + " " + liveMoneyTIPSIndustry(row.Standard)
		event := LiveMoneyEvent{ID: liveMoneyID("tips", row.ID), Kind: LiveMoneyAccelerator, Stage: LiveMoneyAccelerator,
			Company: row.Name, Region: liveMoneyRegionAS, Country: "KR", Date: row.SelYear + "-01-01",
			Investors: row.Operator, Title: "TIPS: " + row.Name, URL: liveMoneyTIPSCard + url.QueryEscape(row.ID),
			Source: liveMoneyTIPSSource, License: liveMoneyTIPSLicense, Quote: firstRunes(quote, 200),
			Text: firstRunes(industry+" "+intro, liveMoneyTextLimit)}
		if event.Rubric = liveRubricOf(rubrics, event.Text); event.Rubric != "" {
			event.RubricBasis = liveMoneyBasisText
		}
		events = append(events, event)
	}
	return events
}

// liveMoneyTIPSIndustry переводит коды отраслей реестра в слова каталога рубрик: корейское описание каталог
// не читает. Коды без перевода остаются как есть.
func liveMoneyTIPSIndustry(codes string) string {
	words := map[string]string{"AI": "artificial intelligence", "CONTENT": "digital content", "BIO": "biotech",
		"BIO_MED": "biotech", "HEALTH": "healthcare", "QUANTUM": "quantum", "ECO": "environment", "ROBOT": "robot",
		"MOBILITY": "vehicle", "SEMI": "semiconductor", "ENERGY": "energy", "ENERGY_RES": "energy resources",
		"ICT": "software", "MECH_MAT": "material", "CHEM": "chemical"}
	var list []string
	if json.Unmarshal([]byte(codes), &list) != nil {
		return codes
	}
	for i, code := range list {
		list[i] = cmp.Or(words[code], code)
	}
	return strings.Join(list, " ")
}
