package application

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Деньги развития по стандарту IATI: обязательства институтов развития и фондов в Африке, Азии, на Ближнем
// Востоке, в Латинской Америке и СНГ — там, где лент о венчурных сделках почти нет. Это не раунд стартапа,
// поэтому событие получает свой вид. Читаем через d-portal: запрос без ключа и с группировкой на сервере.

const (
	liveMoneyIATIURL     = "https://d-portal.org/q.json"
	liveMoneyIATICard    = "https://d-portal.org/ctrack.html#view=act&aid="
	liveMoneyIATISource  = "IATI: институты развития и фонды"
	liveMoneyIATILicense = "лицензия издателя в реестре IATI: CC BY, ODC-BY, ODC-PDDL или с указанием авторства"
	// d-portal молча обрезает ответ на 20 000 строках: упёрлись в предел — значит, часть потеряна.
	liveMoneyIATIRows = 20000
	// Отрасли DAC (три цифры — группа): частный сектор и инновации — связь, финансы, бизнес-услуги,
	// промышленность с технологическими НИОКР и научные организации (43082).
	liveMoneyIATIInnovation = " 220 240 250 321 43082 "
	// К ним — здравоохранение, энергетика без ископаемой генерации и сельское хозяйство: там у радара технологии.
	liveMoneyIATIImpact = liveMoneyIATIInnovation + "121 122 123 231 232 234 236 311 "
)

type liveMoneyIATIPublisher struct {
	name, license string
	// sectors пусто, когда издатель вкладывает только в частные компании и отрасли DAC не ведёт (у IFC — NAICS).
	sectors string
}

// Лицензии — из реестра IATI на 19.10.2026. Hewlett не взят: отраслей DAC не ведёт, а гранты его — о политике
// и образовании.
func liveMoneyIATIPublishers() map[string]liveMoneyIATIPublisher {
	const attribution = "с указанием авторства, "
	return map[string]liveMoneyIATIPublisher{
		"XM-DAC-903":      {"IFC", attribution + "IFC", ""},
		"XM-DAC-46012":    {"IDB Invest", "CC BY, IDB Invest", ""},
		"GB-COH-03877777": {"BII", "CC BY, British International Investment", ""},
		"XI-IATI-EBRD":    {"EBRD", "ODC-BY, EBRD", liveMoneyIATIImpact},
		"XM-DAC-46002":    {"AfDB", "CC BY, AfDB", liveMoneyIATIImpact},
		"XM-DAC-46004":    {"ADB", "CC BY, ADB", liveMoneyIATIImpact},
		"XI-IATI-IADB":    {"IDB", attribution + "IDB", liveMoneyIATIImpact},
		// Всемирный банк кредитует государства: в слой идут только частный сектор и инновации.
		"44000":           {"World Bank", attribution + "World Bank", liveMoneyIATIInnovation},
		"DAC-1601":        {"Gates Foundation", "ODC-BY, Gates Foundation", liveMoneyIATIImpact},
		"XM-DAC-301-2":    {"IDRC", "ODC-BY, IDRC", liveMoneyIATIImpact},
		"NL-KVK-27378529": {"RVO", "ODC-PDDL, RVO", liveMoneyIATIImpact},
	}
}

type liveMoneyIATIRowsOf[T any] struct {
	Rows []T `json:"rows"`
}

type liveMoneyIATIActivity struct {
	AID         string  `json:"aid"`
	Publisher   string  `json:"reporting_ref"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	USD         float64 `json:"sum_trans_usd"`
	Day         int64   `json:"max_trans_day"`
}

type liveMoneyIATICountry struct {
	AID     string  `json:"aid"`
	Code    string  `json:"country_code"`
	Percent float64 `json:"max_country_percent"`
}

type liveMoneyIATISector struct {
	AID  string `json:"aid"`
	Code string `json:"sector_code"`
}

// liveMoneyIATI берёт обязательства (транзакция C) за окно тремя запросами: страны и отрасли отдельно, иначе
// соединение размножило бы суммы.
func liveMoneyIATI(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return nil, 0, fmt.Errorf("iati: window: %w", err)
	}
	end, err := time.Parse(time.DateOnly, through)
	if err != nil {
		return nil, 0, fmt.Errorf("iati: window: %w", err)
	}
	publishers := liveMoneyIATIPublishers()
	filter := url.Values{"trans_code": {"C"}, "reporting_ref": {strings.Join(slices.Sorted(maps.Keys(publishers)), "|")},
		"trans_day_gteq": {strconv.FormatInt(start.Unix()/86400, 10)}, "trans_day_lteq": {strconv.FormatInt(end.Unix()/86400, 10)},
		"limit": {strconv.Itoa(liveMoneyIATIRows)}}
	var activities liveMoneyIATIRowsOf[liveMoneyIATIActivity]
	var countries liveMoneyIATIRowsOf[liveMoneyIATICountry]
	var sectors liveMoneyIATIRowsOf[liveMoneyIATISector]
	for _, part := range []struct {
		from, fields, groupby string
		out                   any
	}{
		{"act,trans", "aid,reporting_ref,title,description,sum_trans_usd,max_trans_day",
			"aid,reporting_ref,title,description", &activities},
		{"act,trans,country", "aid,country_code,max_country_percent", "aid,country_code", &countries},
		{"act,trans,sector", "aid,sector_code", "aid,sector_code", &sectors},
	} {
		query := maps.Clone(filter)
		query.Set("from", part.from)
		query.Set("select", part.fields)
		query.Set("groupby", part.groupby)
		if err = liveMoneyJSON(ctx, client, http.MethodGet, liveMoneyIATIURL+"?"+query.Encode(), nil, part.out); err != nil {
			return nil, 0, fmt.Errorf("iati: %s: %w", part.from, err)
		}
	}
	events := liveMoneyIATIEvents(rubrics, publishers, activities.Rows, countries.Rows, sectors.Rows)
	if max(len(activities.Rows), len(countries.Rows), len(sectors.Rows)) >= liveMoneyIATIRows {
		return events, len(activities.Rows), fmt.Errorf("iati: ответ упёрся в %d строк; остаток не прочитан", liveMoneyIATIRows)
	}
	return events, len(activities.Rows), nil
}

// liveMoneyIATIEvents ставит событие в страну с наибольшей долей вне США, Канады и Европы: там карта и так
// полна, а региональные проекты без страны не попадают никуда, чтобы не выдумывать страну.
func liveMoneyIATIEvents(rubrics []liveRubric, publishers map[string]liveMoneyIATIPublisher,
	activities []liveMoneyIATIActivity, countries []liveMoneyIATICountry, sectors []liveMoneyIATISector) []LiveMoneyEvent {
	regions := liveMoneyRegions()
	recipient := map[string]liveMoneyIATICountry{}
	for _, row := range countries {
		if region := regions[row.Code]; region == "" || region == liveMoneyRegionUS || region == liveMoneyRegionEU {
			continue
		}
		if best, seen := recipient[row.AID]; !seen || row.Percent > best.Percent {
			recipient[row.AID] = row
		}
	}
	codes := map[string][]string{}
	for _, row := range sectors {
		codes[row.AID] = append(codes[row.AID], row.Code)
	}
	events := []LiveMoneyEvent{}
	for _, activity := range activities {
		publisher, known := publishers[activity.Publisher]
		country, placed := recipient[activity.AID]
		if !known || !placed || activity.Title == "" || !liveMoneyIATIRelevant(publisher.sectors, codes[activity.AID]) {
			continue
		}
		event := liveMoneyGrant(rubrics, liveMoneyIATISource, activity.Title, activity.Description)
		event.Kind, event.Stage, event.ID = LiveMoneyDevelopment, LiveMoneyDevelopment, liveMoneyID("iati", activity.AID)
		event.Country, event.Region = country.Code, regions[country.Code]
		event.Date = time.Unix(activity.Day*86400, 0).UTC().Format(time.DateOnly)
		event.Investors, event.License = publisher.name, publisher.license
		event.URL = liveMoneyIATICard + url.QueryEscape(activity.AID)
		if activity.USD > 0 {
			event.Amount = liveMoneyUSD(activity.USD)
		}
		events = append(events, event)
	}
	return events
}

func liveMoneyIATIRelevant(sectors string, codes []string) bool {
	return sectors == "" || slices.ContainsFunc(codes, func(code string) bool {
		return strings.Contains(sectors, " "+code[:min(len(code), 3)]+" ") || strings.Contains(sectors, " "+code+" ")
	})
}
