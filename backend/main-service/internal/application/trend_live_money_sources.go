package application

import (
	"bytes"
	"cmp"
	"context"
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Только источники без запрета на сбор: открытый набор, государственные реестры и ленты издателей,
// чьи условия его не запрещают. YC, Techstars, Tech.eu, Inc42 и подобные — только по разрешению.

const (
	liveMoneyFFName        = "Fundraising Fox"
	liveMoneyFFURL         = "https://huggingface.co/datasets/fundraisingfox/venture-capital/resolve/main/rounds.csv"
	liveMoneyNIHURL        = "https://api.reporter.nih.gov/v2/projects/search"
	liveMoneyNSFURL        = "https://api.nsf.gov/services/v1/awards.json"
	liveMoneyHorizonSource = "CORDIS: Horizon Europe — компании-участники"
	liveMoneyRegionUS      = "americas"
	liveMoneyRegionLA      = "latam"
	liveMoneyRegionEU      = "europe"
	liveMoneyRegionME      = "mena"
	liveMoneyRegionAS      = "asia"
	liveMoneyRegionAF      = "africa"
	liveMoneyRegionCIS     = "cis"
	liveMoneyUnknown       = "unknown"
	liveMoneyTextLimit     = 1200
	liveMoneyReadLimit     = 64 << 20

	liveMoneyKindDataset  = "dataset"
	liveMoneyKindRegistry = "registry"
	liveMoneyKindFeed     = "feed"
	liveMoneyPublicUS     = "общественное достояние США"
)

type liveMoneyFeed struct {
	name    string
	region  string
	license string
}

// Имена совпадают с infra/trend-sources.json: по ним сборщик отдаёт записи ленты.
func liveMoneyFeeds() []liveMoneyFeed {
	const open = "лента издателя: условия не запрещают сбор; хранятся факт сделки, ссылка и цитата"
	return []liveMoneyFeed{
		{"ArcticStartup: раунды Северной Европы", liveMoneyRegionEU, open},
		{"The Recursive: сделки Центральной и Восточной Европы", liveMoneyRegionEU, open},
		{"FrenchWeb: сделки Франции", liveMoneyRegionEU, open},
		{"LatamList: раунды Латинской Америки", liveMoneyRegionLA, open},
		{"LAVCA: венчур Латинской Америки", liveMoneyRegionLA, open},
		{"Swipeline: сделки Турции и мира", liveMoneyRegionME, open},
		{"Launch Base Africa: сделки Африки", liveMoneyRegionAF, open},
		{"Africa: The Big Deal: обзоры сделок Африки", liveMoneyRegionAF, open},
		{"Platum: инвестиции в стартапы Кореи", liveMoneyRegionAS, open},
		{"DailySocial: сделки Юго-Восточной Азии", liveMoneyRegionAS, open},
		{"MedCity News: раунды в медицине", liveMoneyRegionUS, open},
		{"ConTech Roundup: сделки в строительстве", liveMoneyUnknown, open},
		{"TechNode: сделки Китая", liveMoneyRegionAS, open},
		{"Venture Square: инвестиции в стартапы Кореи", liveMoneyRegionAS, open},
		{"egirisim: сделки Турции", liveMoneyRegionME, open},
		{"El Referente: сделки Испании", liveMoneyRegionEU, open},
		{"Disrupt Africa: сделки Африки", liveMoneyRegionAF, open},
		{"HIT Consultant: раунды в цифровой медицине", liveMoneyRegionUS, open},
		{"The Quantum Insider: сделки в квантовых технологиях", liveMoneyUnknown, open},
		{"The Robot Report: сделки в робототехнике", liveMoneyUnknown, open},
		{"vc.ru", liveMoneyRegionCIS, open},
		{"Rusbase", liveMoneyRegionCIS, open},
		{"Inc. Russia", liveMoneyRegionCIS, open},
		{"THE TECH: стартапы Казахстана и Центральной Азии", liveMoneyRegionCIS, open},
		{"DigitalBusiness.kz: ИТ-рынок Казахстана", liveMoneyRegionCIS, open},
		{"Scenius LATAM: еженедельные сделки Латинской Америки", liveMoneyRegionLA, open},
		{"Startupi: стартапы Бразилии", liveMoneyRegionLA, open},
		{"MENA Startup Digest: сделки Ближнего Востока", liveMoneyRegionME, open},
		// Сводки по нескольким регионам: регион ставится по заголовку выпуска.
		{"Techloy: сводки сделок Азии, Африки, Ближнего Востока и Латинской Америки", liveMoneyUnknown, open},
	}
}

func (s *TrendLiveService) buildMoney(ctx context.Context) *LiveMoneyBoard {
	now := s.now().UTC()
	from, through, _ := liveMoneyWindow(now)
	board := &LiveMoneyBoard{BuiltAt: now, From: from, Through: through,
		Method: "early-money-rules-v1+" + liveRubricMethod, Events: []LiveMoneyEvent{}, Sources: []LiveMoneySource{}}
	rubrics := liveRubrics()
	client := &http.Client{Transport: liveMarketTransport(), Timeout: 3 * time.Minute}
	add := func(source LiveMoneySource, events []LiveMoneyEvent, records int, err error) {
		source.Records, source.Events = records, len(events)
		if err != nil {
			source.Error = err.Error()
		}
		board.Sources = append(board.Sources, source)
		board.Events = append(board.Events, events...)
	}
	events, records, err := liveMoneyFundraisingFox(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyFFName, Kind: liveMoneyKindDataset, Region: liveMoneyUnknown, URL: liveMoneyFFURL,
		License: "CC BY 4.0, Fundraising Fox"}, events, records, err)
	events, records, err = liveMoneyNIH(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: "NIH RePORTER: SBIR/STTR", Kind: liveMoneyKindRegistry, Region: liveMoneyRegionUS,
		URL: liveMoneyNIHURL, License: liveMoneyPublicUS}, events, records, err)
	events, records, err = liveMoneyNIHForeign(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyNIHForeignSource, Kind: liveMoneyKindRegistry, Region: liveMoneyUnknown,
		URL: liveMoneyNIHURL, License: liveMoneyPublicUS}, events, records, err)
	events, records, err = liveMoneyNSF(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: "NSF Awards: SBIR/STTR", Kind: liveMoneyKindRegistry, Region: liveMoneyRegionUS, URL: liveMoneyNSFURL,
		License: liveMoneyPublicUS}, events, records, err)
	events, records, err = s.liveMoneyHorizon(rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyHorizonSource, Kind: liveMoneyKindRegistry, Region: liveMoneyRegionEU,
		URL: liveCORDISDump, License: "CC BY 4.0, Европейская комиссия"}, events, records, err)
	events, records, err = liveMoneyVinnova(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyVinnovaSource, Kind: liveMoneyKindRegistry, Region: liveMoneyRegionEU,
		URL: liveMoneyVinnovaURL, License: liveMoneyVinnovaLicense}, events, records, err)
	events, records, err = liveMoneyIATI(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyIATISource, Kind: liveMoneyKindRegistry, Region: liveMoneyUnknown,
		URL: liveMoneyIATIURL, License: liveMoneyIATILicense}, events, records, err)
	events, records, err = liveMoneyFSI(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyFSISource, Kind: liveMoneyKindRegistry, Region: liveMoneyRegionCIS,
		URL: liveMoneyFSIURL, License: liveMoneyFSILicense}, events, records, err)
	events, records, err = liveMoneyTIPS(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyTIPSSource, Kind: liveMoneyKindRegistry, Region: liveMoneyRegionAS,
		URL: liveMoneyTIPSURL, License: liveMoneyTIPSLicense}, events, records, err)
	events, records, err = liveMoneyTWSBIR(ctx, client, rubrics, from, through)
	add(LiveMoneySource{Name: liveMoneyTWSBIRSource, Kind: liveMoneyKindRegistry, Region: liveMoneyRegionAS,
		URL: liveMoneyTWSBIRPage, License: liveMoneyTWSBIRLicense}, events, records, err)
	for _, feed := range liveMoneyFeeds() {
		events, records, err = liveMoneyFeedEvents(ctx, client, rubrics, feed, from, through)
		add(LiveMoneySource{Name: feed.name, Kind: liveMoneyKindFeed, Region: feed.region, License: feed.license}, events, records, err)
	}
	// Регион — по стране компании; регион источника остаётся запасным, когда страны в записи нет.
	regions := liveMoneyRegions()
	for i := range board.Events {
		board.Events[i].Region = liveMoneyRegion(regions, board.Events[i].Country, board.Events[i].Region)
	}
	board.Events = liveMoneyDedupe(liveMoneyTidy(board.Events))
	return board
}

// liveMoneyDedupe склеивает одну сделку из набора и из ленты: оставляет запись с цитатой и рубрикой.
func liveMoneyDedupe(events []LiveMoneyEvent) []LiveMoneyEvent {
	slices.SortStableFunc(events, func(a, b LiveMoneyEvent) int {
		return cmp.Or(strings.Compare(b.Date, a.Date), liveMoneyRank(b)-liveMoneyRank(a))
	})
	seen := map[string]bool{}
	out := events[:0]
	for _, event := range events {
		key := event.ID
		// Реестр сам даёт событию уникальный номер: два гранта компании за месяц — два гранта, а не повтор.
		if company := strings.ToLower(strings.TrimSpace(event.Company)); company != "" &&
			(event.Kind == LiveMoneyRound || event.Kind == LiveMoneyAccelerator) {
			key = company + "|" + event.Date[:min(len(event.Date), 7)] + "|" + event.Kind
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, event)
	}
	return out
}

func liveMoneyRank(event LiveMoneyEvent) int {
	rank := 0
	if event.Rubric != "" {
		rank += 2
	}
	if event.Quote != "" {
		rank++
	}
	return rank
}

func liveMoneyFundraisingFox(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	var buf bytes.Buffer
	request := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, liveMoneyFFURL, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("fundraising fox: build request: %w", err)
		}
		req.Header.Set("User-Agent", liveEventAgent)
		return req, nil
	}
	restart := func() error { buf.Reset(); return nil }
	if err := liveMarketRead(ctx, client, request, &buf, restart, liveMoneyReadLimit); err != nil {
		return nil, 0, fmt.Errorf("fundraising fox: download: %w", err)
	}
	reader := csv.NewReader(&buf)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("fundraising fox: header: %w", err)
	}
	column := map[string]int{}
	for i, name := range header {
		column[strings.TrimPrefix(name, "\ufeff")] = i
	}
	field := func(row []string, name string) string {
		if i, ok := column[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	rules := liveMoneyRules()
	events, records := []LiveMoneyEvent{}, 0
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return events, records, fmt.Errorf("fundraising fox: row: %w", readErr)
		}
		date := field(row, "announced_on")
		if date < from || date > through {
			continue
		}
		records++
		if event, ok := liveMoneyFFEvent(rules, rubrics, func(name string) string { return field(row, name) }); ok {
			events = append(events, event)
		}
	}
	return events, records, nil
}

// liveMoneyFFEvent отбрасывает Series B и позже: это уже не ранние деньги, а масштабирование.
func liveMoneyFFEvent(rules liveMoneyPatterns, rubrics []liveRubric, field func(string) string) (LiveMoneyEvent, bool) {
	label := cmp.Or(field("stage"), field("round_label"))
	if rules.reject.MatchString(label) || strings.EqualFold(label, "growth") {
		return LiveMoneyEvent{}, false
	}
	company, domain := field("company_name"), field("domain")
	event := LiveMoneyEvent{Kind: LiveMoneyRound, Stage: liveMoneyStage(rules, label), Company: company,
		Region: liveMoneyUnknown, Date: field("announced_on"), Title: strings.TrimSpace(company + " " + label),
		URL: field("fundraisingfox_url"), Source: liveMoneyFFName, License: "CC BY 4.0, Fundraising Fox",
		Text: company + " " + domain}
	event.ID = liveMoneyID(event.Source, event.URL, event.Date, label)
	if amount, err := strconv.ParseFloat(field("amount_usd"), 64); err == nil && amount > 0 {
		event.Amount = liveMoneyUSD(amount)
	}
	// Подача Form D — это всегда компания США; у пресс-записи в наборе нет ни страны, ни инвесторов.
	if event.Country = liveMoneyDomainCountry(domain); field("basis") == "sec_filing" {
		event.Country = "US"
	}
	if event.Rubric = liveRubricOf(rubrics, company+" "+strings.ReplaceAll(domain, ".", " ")); event.Rubric != "" {
		event.RubricBasis = liveMoneyBasisName
	}
	return event, true
}

// liveMoneyIn пишет сумму в валюте реестра тем же сокращением, что и доллары: пересчёт по курсу исказил бы сумму.
func liveMoneyIn(currency string, amount float64) string {
	return currency + strings.TrimPrefix(liveMoneyUSD(amount), "$")
}

func liveMoneyUSD(amount float64) string {
	switch {
	case amount >= 1e9:
		return "$" + strconv.FormatFloat(amount/1e9, 'f', 1, 64) + "B"
	case amount >= 1e6:
		return "$" + strconv.FormatFloat(amount/1e6, 'f', 1, 64) + "M"
	}
	return "$" + strconv.FormatFloat(amount/1e3, 'f', 0, 64) + "K"
}

type liveMoneyNIHPage struct {
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
	Results []liveMoneyNIHProject `json:"results"`
}

type liveMoneyNIHProject struct {
	ApplID       int64  `json:"appl_id"`
	Title        string `json:"project_title"`
	Abstract     string `json:"abstract_text"`
	Amount       int64  `json:"award_amount"`
	Start        string `json:"project_start_date"`
	Notice       string `json:"award_notice_date"`
	ActivityCode string `json:"activity_code"`
	AgencyCode   string `json:"agency_code"`
	URL          string `json:"project_detail_url"`
	Organization struct {
		Name    string `json:"org_name"`
		Country string `json:"org_country"`
	} `json:"organization"`
}

type liveMoneyNIHRequest struct {
	Criteria struct {
		ActivityCodes      []string          `json:"activity_codes,omitempty"`
		StartDate          map[string]string `json:"project_start_date,omitempty"`
		NoticeDate         map[string]string `json:"award_notice_date,omitempty"`
		OrgCountries       []string          `json:"org_countries,omitempty"`
		ExcludeSubprojects bool              `json:"exclude_subprojects,omitempty"`
	} `json:"criteria"`
	IncludeFields []string `json:"include_fields"`
	Offset        int      `json:"offset"`
	Limit         int      `json:"limit"`
}

func liveMoneyNIHQuery(from, through string) liveMoneyNIHRequest {
	var query liveMoneyNIHRequest
	query.Criteria.ActivityCodes = []string{"R41", "R42", "R43", "R44", "U43", "U44", "SB1"}
	query.Criteria.StartDate = map[string]string{"from_date": from, "to_date": through}
	query.IncludeFields = liveMoneyNIHFields()
	return query
}

// liveMoneyNIHFields общий для гранта малому бизнесу и зарубежного гранта: дату события каждый берёт свою.
func liveMoneyNIHFields() []string {
	return []string{"ApplId", "ProjectTitle", "AbstractText", "Organization", "AwardAmount", "ProjectStartDate",
		"AwardNoticeDate", "ActivityCode", "AgencyCode", "ProjectDetailUrl"}
}

// liveMoneyNIH: R41–R44, U43/U44 и SB1 — коды малого бизнеса NIH; получатель по закону — компания США.
func liveMoneyNIH(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	events := []LiveMoneyEvent{}
	total, err := liveMoneyNIHSearch(ctx, client, liveMoneyNIHQuery(from, through), func(row liveMoneyNIHProject) {
		event := liveMoneyGrant(rubrics, "NIH RePORTER: SBIR/STTR", row.Title, row.Abstract)
		event.ID = liveMoneyID("nih", strconv.FormatInt(row.ApplID, 10))
		event.Company, event.Date, event.URL = row.Organization.Name, row.Start[:min(len(row.Start), 10)], row.URL
		event.Region, event.Country, event.Investors = liveMoneyRegionUS, "US", "NIH, "+row.ActivityCode
		if row.Amount > 0 {
			event.Amount = liveMoneyUSD(float64(row.Amount))
		}
		events = append(events, event)
	})
	return events, total, err
}

func liveMoneyNIHSearch(ctx context.Context, client *http.Client, query liveMoneyNIHRequest,
	visit func(liveMoneyNIHProject)) (int, error) {
	total := 0
	for query.Offset, query.Limit = 0, 500; query.Offset == 0 || query.Offset < total; query.Offset += 500 {
		body, err := json.Marshal(query)
		if err != nil {
			return total, fmt.Errorf("nih: encode: %w", err)
		}
		var page liveMoneyNIHPage
		if err = liveMoneyJSON(ctx, client, http.MethodPost, liveMoneyNIHURL, body, &page); err != nil {
			return total, fmt.Errorf("nih: %w", err)
		}
		total = page.Meta.Total
		for _, row := range page.Results {
			visit(row)
		}
		if len(page.Results) == 0 {
			break
		}
	}
	return total, nil
}

type liveMoneyNSFPage struct {
	Response struct {
		Metadata struct {
			TotalCount int `json:"totalCount"`
		} `json:"metadata"`
		Award []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Abstract string `json:"abstractText"`
			Awardee  string `json:"awardeeName"`
			Country  string `json:"awardeeCountryCode"`
			Amount   string `json:"estimatedTotalAmt"`
			Date     string `json:"date"`
			Program  string `json:"fundProgramName"`
		} `json:"award"`
	} `json:"response"`
}

// liveMoneyNSF не сохраняет руководителей и их почту, хотя API их отдаёт: слою нужна компания.
func liveMoneyNSF(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return nil, 0, fmt.Errorf("nsf: window: %w", err)
	}
	events, total := []LiveMoneyEvent{}, 0
	for offset := 1; offset == 1 || offset <= total; offset += 25 {
		query := url.Values{"fundProgramName": {"SBIR Phase I"}, "dateStart": {start.Format("01/02/2006")},
			"rpp": {"25"}, "offset": {strconv.Itoa(offset)},
			"printFields": {"id,title,abstractText,awardeeName,awardeeCountryCode,estimatedTotalAmt,date,fundProgramName"}}
		var page liveMoneyNSFPage
		if err = liveMoneyJSON(ctx, client, http.MethodGet, liveMoneyNSFURL+"?"+query.Encode(), nil, &page); err != nil {
			return events, total, fmt.Errorf("nsf: %w", err)
		}
		total = page.Response.Metadata.TotalCount
		for _, row := range page.Response.Award {
			date, parseErr := time.Parse("01/02/2006", row.Date)
			if parseErr != nil || date.Format(time.DateOnly) > through {
				continue
			}
			event := liveMoneyGrant(rubrics, "NSF Awards: SBIR/STTR", row.Title, row.Abstract)
			event.ID, event.Company, event.Date = liveMoneyID("nsf", row.ID), row.Awardee, date.Format(time.DateOnly)
			event.URL = "https://www.nsf.gov/awardsearch/showAward?AWD_ID=" + url.QueryEscape(row.ID)
			event.Region, event.Country, event.Investors = liveMoneyRegionUS, cmp.Or(row.Country, "US"), "NSF, "+row.Program
			if amount, convErr := strconv.ParseFloat(row.Amount, 64); convErr == nil && amount > 0 {
				event.Amount = liveMoneyUSD(amount)
			}
			events = append(events, event)
		}
		if len(page.Response.Award) == 0 {
			break
		}
	}
	return events, total, nil
}

// liveMoneyHorizon даёт событие на каждую компанию-участника проекта Horizon Europe: грант получает каждая,
// а страна участника выводит на карту Турцию, Израиль, Африку и Латинскую Америку. Выгрузку второй раз не качает.
func (s *TrendLiveService) liveMoneyHorizon(rubrics []liveRubric, from, through string) ([]LiveMoneyEvent, int, error) {
	index := &s.eventData.cordis
	index.mu.Lock()
	dump := index.dump
	index.mu.Unlock()
	if dump == nil {
		return []LiveMoneyEvent{}, 0, errors.New("выгрузка CORDIS ещё загружается в фоне")
	}
	return liveMoneyHorizonEvents(rubrics, dump.Grants, from, through)
}

func liveMoneyHorizonEvents(rubrics []liveRubric, grants []liveGrant, from, through string) ([]LiveMoneyEvent, int, error) {
	events, records := []LiveMoneyEvent{}, 0
	for _, grant := range grants {
		if grant.Date < from || grant.Date > through || len(grant.Participants) == 0 {
			continue
		}
		records++
		base := liveMoneyGrant(rubrics, liveMoneyHorizonSource, grant.Title, grant.Text)
		base.Date, base.URL, base.Region = grant.Date, grant.URL, liveMoneyRegionEU
		base.Investors, base.License = "Horizon Europe, "+grant.Phase, "CC BY 4.0, Европейская комиссия"
		for _, participant := range grant.Participants {
			event := base
			event.ID = liveMoneyID(grant.ID, participant.Name)
			event.Company, event.Country = participant.Name, liveMoneyCountry(participant.Country)
			if participant.Amount > 0 {
				event.Amount = liveMoneyIn("€", participant.Amount)
			}
			events = append(events, event)
		}
	}
	return events, records, nil
}

func liveMoneyGrant(rubrics []liveRubric, source, title, abstract string) LiveMoneyEvent {
	text := firstRunes(strings.Join(strings.Fields(title+". "+abstract), " "), liveMoneyTextLimit)
	event := LiveMoneyEvent{Kind: LiveMoneyGrant, Stage: LiveMoneyGrant, Title: title, Source: source,
		License: liveMoneyPublicUS, Quote: title, Text: text}
	if event.Rubric = liveRubricOf(rubrics, text); event.Rubric != "" {
		event.RubricBasis = liveMoneyBasisText
	}
	return event
}

type liveMoneyRecordsPage struct {
	Records []struct {
		Title string `json:"title"`
		Text  string `json:"text"`
		URL   string `json:"url"`
		Date  string `json:"date"`
	} `json:"records"`
	NextCursor string `json:"next_cursor"`
}

// liveMoneyFeedEvents листает записи одной ленты в сборщике; без сборщика слой честно остаётся без лент.
func liveMoneyFeedEvents(ctx context.Context, client *http.Client, rubrics []liveRubric, feed liveMoneyFeed,
	from, through string) ([]LiveMoneyEvent, int, error) {
	base := strings.TrimRight(os.Getenv("WEB_COLLECTOR_URL"), "/")
	if base == "" {
		return []LiveMoneyEvent{}, 0, errors.New("WEB_COLLECTOR_URL не задан")
	}
	rules := liveMoneyRules()
	events, records, cursor := []LiveMoneyEvent{}, 0, ""
	for range 100 {
		query := url.Values{"source": {feed.name}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var result liveMoneyRecordsPage
		address := base + "/v1/records?query=*&limit=100&from=" + from + "&through=" + through + "&" + query.Encode()
		if err := liveMoneyJSON(ctx, client, http.MethodGet, address, nil, &result); err != nil {
			return events, records, fmt.Errorf("лента %s: %w", feed.name, err)
		}
		for _, record := range result.Records {
			records++
			found := liveMoneyFromRecord(rules, rubrics, feed, record.Title, record.Text, record.URL, record.Date)
			if feed.region == liveMoneyUnknown {
				for i := range found {
					found[i].Region = liveMoneyRegionFromTitle(record.Title)
				}
			}
			events = append(events, found...)
		}
		if cursor = result.NextCursor; cursor == "" {
			return events, records, nil
		}
	}
	return events, records, errors.New("лента длиннее 10 000 записей; остаток не прочитан")
}

// liveMoneyRegionFromTitle — регион выпуска сводки по его заголовку («Africa's Startup Funding This Week»);
// без слова-региона событие остаётся без региона, а не попадает в чужой.
func liveMoneyRegionFromTitle(title string) string {
	lower := strings.ToLower(title)
	for _, probe := range []struct{ word, region string }{
		{"africa", liveMoneyRegionAF}, {"middle east", liveMoneyRegionME}, {"mena", liveMoneyRegionME}, {"gulf", liveMoneyRegionME},
		{"latin america", liveMoneyRegionLA}, {"latam", liveMoneyRegionLA}, {"asia", liveMoneyRegionAS}, {"europe", liveMoneyRegionEU},
		{"russia", liveMoneyRegionCIS}, {"central asia", liveMoneyRegionCIS},
	} {
		if strings.Contains(lower, probe.word) {
			return probe.region
		}
	}
	return liveMoneyUnknown
}

func liveMoneyJSON(ctx context.Context, client *http.Client, method, address string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", liveEventAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err = json.UnmarshalRead(io.LimitReader(resp.Body, liveMoneyReadLimit), out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
