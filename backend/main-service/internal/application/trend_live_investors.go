package application

import (
	"bytes"
	"cmp"
	"compress/gzip"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Справочник ранних инвесторов собран скриптами вне репозитория и вшит в бинарник:
// мегабайт сжатого JSON меняется раз в месяцы, отдельное хранилище и сборщик ему не нужны.

//go:embed data/investors-v1.json.gz
var liveInvestorsFile []byte

const (
	liveInvestorsPage     = 50
	liveInvestorsMaxPage  = 100
	liveInvestorsLatest   = 3
	liveInvestorsQueryMax = 100
	liveInvestorsSortName = "name"
)

type LiveInvestorSource struct {
	Source  string `json:"source"`
	URL     string `json:"url"`
	License string `json:"license"`
}

// LiveInvestorDeal — сделка доски ранних денег, где инвестор назван тем же именем, что в справочнике.
type LiveInvestorDeal struct {
	Company string `json:"company,omitempty"`
	Date    string `json:"date"`
	Stage   string `json:"stage"`
	Rubric  string `json:"rubric,omitempty"`
	URL     string `json:"url"`
}

type LiveInvestorDeals struct {
	Total   int                `json:"total"`
	Stages  map[string]int     `json:"stages"`
	Rubrics map[string]int     `json:"rubrics"`
	Latest  []LiveInvestorDeal `json:"latest"`
}

// LiveInvestor — запись справочника; Region и Rubrics считаются при загрузке тем же правилом, что у сделок.
type LiveInvestor struct {
	Name          string               `json:"name"`
	Type          string               `json:"type"`
	Country       string               `json:"country,omitempty"`
	City          string               `json:"city,omitempty"`
	Stages        []string             `json:"stages"`
	Sectors       []string             `json:"sectors"`
	Website       string               `json:"website,omitempty"`
	Registered    string               `json:"registered,omitempty"`
	AUMUSD        float64              `json:"aum_usd,omitzero"`
	Funds         int                  `json:"funds,omitzero"`
	DealsReported int                  `json:"deals_reported,omitzero"`
	NInvestments  int                  `json:"n_investments,omitzero"`
	Sources       []LiveInvestorSource `json:"sources"`
	Region        string               `json:"region"`
	Rubrics       []string             `json:"rubrics"`
	// Deals пуст, пока доска ранних денег не собрана: ноль сделок и «не знаем» — разные ответы.
	Deals *LiveInvestorDeals `json:"deals,omitempty"`
	key   string
	text  string
}

type LiveInvestorFacets struct {
	Regions   map[string]int `json:"regions"`
	Countries map[string]int `json:"countries"`
	Types     map[string]int `json:"types"`
	Stages    map[string]int `json:"stages"`
	Rubrics   map[string]int `json:"rubrics"`
}

type LiveInvestorSourceCount struct {
	Name    string `json:"name"`
	License string `json:"license"`
	Count   int    `json:"count"`
}

type LiveInvestorPage struct {
	Status       string                    `json:"status"`
	BuiltAt      string                    `json:"built_at"`
	Total        int                       `json:"total"`
	Matched      int                       `json:"matched"`
	MoneyStatus  string                    `json:"money_status"`
	MoneyFrom    string                    `json:"money_from,omitempty"`
	MoneyThrough string                    `json:"money_through,omitempty"`
	Facets       LiveInvestorFacets        `json:"facets"`
	Items        []LiveInvestor            `json:"items"`
	Sources      []LiveInvestorSourceCount `json:"sources"`
}

type LiveInvestorQuery struct {
	Region, Country, Type, Stage, Rubric, Q, Sort string
	Offset, Limit                                 int
}

type liveInvestorDirectory struct {
	builtAt string
	items   []LiveInvestor
	byName  map[string][]int
	sources []LiveInvestorSourceCount
}

type liveInvestorIndex struct {
	once      sync.Once
	directory *liveInvestorDirectory
	mu        sync.Mutex
	dealsAt   time.Time
	deals     map[int]LiveInvestorDeals
}

// Investors фильтрует справочник в памяти; фасет считается по записям, прошедшим все остальные фильтры.
func (s *TrendLiveService) Investors(query LiveInvestorQuery) (LiveInvestorPage, error) {
	if !liveInvestorQueryValid(query) {
		return LiveInvestorPage{}, fmt.Errorf("%w: investor filter", ErrInvalidArgument)
	}
	index := &s.eventData.investors
	index.once.Do(func() {
		var err error
		if index.directory, err = liveInvestorsParse(liveInvestorsFile); err != nil {
			slog.Warn("investor directory unreadable", "error", err)
		}
	})
	if index.directory == nil {
		return LiveInvestorPage{Status: LiveSearchFailed, Items: []LiveInvestor{}}, nil
	}
	board, status := s.moneyBoard()
	var deals map[int]LiveInvestorDeals
	if board != nil {
		// Пересвязка — один проход по сделкам доски с поиском имени в словаре: 30 тыс. сделок — 90 мс раз в сутки, фон не нужен.
		index.mu.Lock()
		if index.deals == nil || !index.dealsAt.Equal(board.BuiltAt) {
			index.deals, index.dealsAt = liveInvestorDealsOf(board.Events, index.directory.byName), board.BuiltAt
		}
		deals = index.deals
		index.mu.Unlock()
	}
	page := index.directory.page(query, deals, board != nil)
	page.MoneyStatus = status
	if board != nil {
		page.MoneyFrom, page.MoneyThrough = board.From, board.Through
	}
	return page, nil
}

func liveInvestorQueryValid(query LiveInvestorQuery) bool {
	enum := func(value, allowed string) bool {
		return value == "" || slices.Contains(strings.Fields(allowed), value)
	}
	country := query.Country == "" || len(query.Country) == 2 && strings.Trim(query.Country, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
	rubric := query.Rubric == "" || slices.ContainsFunc(liveRubrics(), func(r liveRubric) bool { return r.ID == query.Rubric })
	return enum(query.Region, "americas europe latam mena africa asia cis unknown") && country && rubric &&
		enum(query.Type, "vc_manager vc_fund seed_fund angel_fund angel_network accelerator cvc government family_office other") &&
		enum(query.Stage, "angel pre_seed seed series_a growth") && enum(query.Sort, "deals name investments") &&
		utf8.RuneCountInString(query.Q) <= liveInvestorsQueryMax && query.Offset >= 0 && query.Limit >= 0 &&
		query.Limit <= liveInvestorsMaxPage
}

func liveInvestorsParse(raw []byte) (*liveInvestorDirectory, error) {
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("investors: open: %w", err)
	}
	defer func() { _ = reader.Close() }()
	var file struct {
		BuiltAt   string         `json:"built_at"`
		Investors []LiveInvestor `json:"investors"`
	}
	if err := json.UnmarshalRead(reader, &file); err != nil {
		return nil, fmt.Errorf("investors: decode: %w", err)
	}
	return liveInvestorsBuild(file.BuiltAt, file.Investors), nil
}

func liveInvestorsBuild(builtAt string, items []LiveInvestor) *liveInvestorDirectory {
	regions, rubrics := liveMoneyRegions(), liveRubrics()
	bySector := map[string]string{}
	directory := &liveInvestorDirectory{builtAt: builtAt, items: items, byName: map[string][]int{}}
	licenses, counts := map[string]string{}, map[string]int{}
	for i := range items {
		item := &items[i]
		item.Region = liveMoneyRegion(regions, item.Country, liveMoneyUnknown)
		item.Rubrics = []string{}
		for _, sector := range item.Sectors {
			rubric, seen := bySector[sector]
			if !seen {
				rubric = liveRubricOf(rubrics, sector+" "+liveInvestorSectorWords(sector))
				bySector[sector] = rubric
			}
			if rubric != "" && !slices.Contains(item.Rubrics, rubric) {
				item.Rubrics = append(item.Rubrics, rubric)
			}
		}
		liveInvestorCountSources(item.Sources, licenses, counts)
		// Реестры пишут сайт и без схемы, и капслоком; без схемы ссылка открылась бы внутри продукта.
		if site := strings.ToLower(item.Website); site != "" && !strings.HasPrefix(site, "http://") &&
			!strings.HasPrefix(site, "https://") {
			item.Website = "https://" + item.Website
		}
		item.key, item.text = liveInvestorKey(item.Name), strings.ToLower(item.Name+" "+item.City)
		if item.key != "" {
			directory.byName[item.key] = append(directory.byName[item.key], i)
		}
	}
	for name, count := range counts {
		directory.sources = append(directory.sources, LiveInvestorSourceCount{Name: name, License: licenses[name], Count: count})
	}
	slices.SortFunc(directory.sources, func(a, b LiveInvestorSourceCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Name, b.Name))
	})
	return directory
}

// liveInvestorCountSources засчитывает сведённую запись «ESMA + EIF» каждому реестру; лицензия берётся у записи,
// где реестр единственный: у сведённой она относится только к первому.
func liveInvestorCountSources(sources []LiveInvestorSource, licenses map[string]string, counts map[string]int) {
	for i := range sources {
		source := &sources[i]
		source.Source = liveInvestorSourceName(source.Source)
		merged := strings.Contains(source.Source, " + ")
		for name := range strings.SplitSeq(source.Source, " + ") {
			counts[name]++
			if _, known := licenses[name]; !known || !merged {
				licenses[name] = source.License
			}
		}
	}
}

// liveInvestorSectorWords добавляет к частой отрасли Fundraising Fox слово каталога рубрик: каталог писан для
// заголовков статей и не узнаёт «Healthtech» или «SaaS» — так же, как liveMoneyTIPSIndustry для кодов TIPS.
// ponytail: покрыты отрасли от сотни фондов и чаще; редкие и национальные названия остаются без рубрики.
func liveInvestorSectorWords(sector string) string {
	switch strings.ToLower(sector) {
	case "healthtech", "digital health", "mental health", "femtech", "longevity", "health", "life sciences",
		"life science", "life science and health", "gezondheidszorg", "medizintechnik", "medizin - healthcare":
		return "healthcare"
	case "saas", "developer tools", "data & infrastructure", "ict", "it", "internet", "software / it", "other digital":
		return "computer software"
	case "crypto & web3":
		return "blockchain"
	case "agtech", "agro/food":
		return "agricultural"
	case "cleantech", "climate/green":
		return "climate change"
	case "space tech", "space":
		return "satellite"
	case "media & entertainment":
		return "digital content"
	case "defense tech":
		return "physical security"
	}
	return ""
}

// liveInvestorSourceName раскрывает служебные коды сборщика: в подписи источника человек ищет название, а не ключ.
func liveInvestorSourceName(code string) string {
	switch code {
	case "ffox":
		return liveMoneyFFName
	case "wikidata":
		return "Wikidata"
	}
	if strings.ContainsRune(code, ' ') {
		return code
	}
	return strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(code, "rpt_"), "_", " "))
}

// liveInvestorKey сводит имя к ключу точного совпадения: регистр, пунктуация и правовая форма в конце не считаются.
// Близкие имена («Lowercarbon» и «Lowercarbon Capital») намеренно не склеиваются: ложная сделка хуже пропущенной.
func liveInvestorKey(name string) string {
	name = strings.NewReplacer(".", "", "'", "", "’", "", "/", "").Replace(strings.ToLower(name))
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	legal := " llc lp llp ltd limited inc incorporated corp corporation co company plc gmbh mbh ag kg sa sas sarl srl " +
		"spa bv nv ab as asa oy oyj pte pty sl "
	for len(words) > 1 && strings.Contains(legal, " "+words[len(words)-1]+" ") {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// liveInvestorDealsOf связывает раунды и отборы акселераторов с инвесторами справочника. У грантов и институтов
// развития в поле Investors программа, а не инвестор, поэтому они в связку не идут.
func liveInvestorDealsOf(events []LiveMoneyEvent, byName map[string][]int) map[int]LiveInvestorDeals {
	deals := map[int]LiveInvestorDeals{}
	for _, event := range events {
		if event.Kind != LiveMoneyRound && event.Kind != LiveMoneyAccelerator {
			continue
		}
		seen := map[int]bool{}
		for chunk := range strings.SplitSeq(strings.ReplaceAll(event.Investors, " and ", ","), ",") {
			for _, i := range byName[liveInvestorKey(chunk)] {
				if seen[i] {
					continue
				}
				seen[i] = true
				deal := deals[i]
				deal.add(&event)
				deals[i] = deal
			}
		}
	}
	return deals
}

func (d *LiveInvestorDeals) add(event *LiveMoneyEvent) {
	if d.Total == 0 {
		d.Stages, d.Rubrics = map[string]int{}, map[string]int{}
	}
	d.Total++
	d.Stages[event.Stage]++
	if event.Rubric != "" {
		d.Rubrics[event.Rubric]++
	}
	d.Latest = append(d.Latest, LiveInvestorDeal{Company: event.Company, Date: event.Date, Stage: event.Stage,
		Rubric: event.Rubric, URL: event.URL})
	slices.SortStableFunc(d.Latest, func(a, b LiveInvestorDeal) int { return strings.Compare(b.Date, a.Date) })
	d.Latest = d.Latest[:min(len(d.Latest), liveInvestorsLatest)]
}

func (d *liveInvestorDirectory) page(query LiveInvestorQuery, deals map[int]LiveInvestorDeals, linked bool) LiveInvestorPage {
	page := LiveInvestorPage{Status: LiveSearchReady, BuiltAt: d.builtAt, Total: len(d.items), Items: []LiveInvestor{},
		Facets: LiveInvestorFacets{Regions: map[string]int{}, Countries: map[string]int{},
			Types: map[string]int{}, Stages: map[string]int{}, Rubrics: map[string]int{}}}
	text := strings.ToLower(strings.TrimSpace(query.Q))
	matched := []int{}
	for i := range d.items {
		item := &d.items[i]
		if text != "" && !strings.Contains(item.text, text) {
			continue
		}
		pass := query.pass(item)
		// Запись, не прошедшая ровно один фильтр, ещё считается в фасет этого фильтра: так видно, куда можно перейти.
		failed := liveInvestorFailed(pass)
		for facet, ok := range pass {
			if failed == 0 || failed == 1 && !ok {
				page.Facets.add(facet, item)
			}
		}
		if failed == 0 {
			matched = append(matched, i)
		}
	}
	page.Matched = len(matched)
	page.Sources = d.matchedSources(matched)
	d.sort(matched, deals, query.Sort)
	limit := cmp.Or(query.Limit, liveInvestorsPage)
	for _, i := range matched[min(query.Offset, len(matched)):min(query.Offset+limit, len(matched))] {
		item := d.items[i]
		if linked {
			deal := deals[i]
			item.Deals = &deal
		}
		page.Items = append(page.Items, item)
	}
	return page
}

// matchedSources — источники только выбранных записей: подпись «откуда данные» у страны не должна показывать весь мир.
func (d *liveInvestorDirectory) matchedSources(matched []int) []LiveInvestorSourceCount {
	licenses := make(map[string]string, len(d.sources))
	for _, source := range d.sources {
		licenses[source.Name] = source.License
	}
	counts := map[string]int{}
	for _, i := range matched {
		for _, source := range d.items[i].Sources {
			for name := range strings.SplitSeq(source.Source, " + ") {
				counts[name]++
			}
		}
	}
	sources := make([]LiveInvestorSourceCount, 0, len(counts))
	for name, count := range counts {
		sources = append(sources, LiveInvestorSourceCount{Name: name, License: licenses[name], Count: count})
	}
	slices.SortFunc(sources, func(a, b LiveInvestorSourceCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Name, b.Name))
	})
	return sources
}

// pass отвечает по каждому фильтру отдельно, в порядке полей LiveInvestorFacets.
func (q *LiveInvestorQuery) pass(item *LiveInvestor) [5]bool {
	return [...]bool{q.Region == "" || item.Region == q.Region, q.Country == "" || item.Country == q.Country,
		q.Type == "" || item.Type == q.Type, q.Stage == "" || slices.Contains(item.Stages, q.Stage),
		q.Rubric == "" || slices.Contains(item.Rubrics, q.Rubric)}
}

func liveInvestorFailed(pass [5]bool) int {
	failed := 0
	for _, ok := range pass {
		if !ok {
			failed++
		}
	}
	return failed
}

func (d *liveInvestorDirectory) sort(matched []int, deals map[int]LiveInvestorDeals, by string) {
	total := func(i int) int { return deals[i].Total }
	slices.SortFunc(matched, func(a, b int) int {
		x, y := &d.items[a], &d.items[b]
		switch by {
		case liveInvestorsSortName:
			return cmp.Or(strings.Compare(x.key, y.key), cmp.Compare(a, b))
		case "investments":
			return cmp.Or(cmp.Compare(y.NInvestments, x.NInvestments), cmp.Compare(total(b), total(a)),
				strings.Compare(x.key, y.key), cmp.Compare(a, b))
		}
		return cmp.Or(cmp.Compare(total(b), total(a)), cmp.Compare(y.NInvestments, x.NInvestments),
			strings.Compare(x.key, y.key), cmp.Compare(a, b))
	})
}

func (f *LiveInvestorFacets) add(facet int, item *LiveInvestor) {
	switch facet {
	case 0:
		f.Regions[item.Region]++
	case 1:
		if item.Country != "" {
			f.Countries[item.Country]++
		}
	case 2:
		f.Types[item.Type]++
	case 3:
		for _, stage := range item.Stages {
			f.Stages[stage]++
		}
	default:
		for _, rubric := range item.Rubrics {
			f.Rubrics[rubric]++
		}
	}
}
