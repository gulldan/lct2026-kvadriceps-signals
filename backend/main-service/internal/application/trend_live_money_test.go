package application

import (
	"encoding/json/v2"
	"math"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestLiveMoneySummaryFiltersBeforeLatestLimit(t *testing.T) {
	t.Parallel()
	s := &TrendLiveService{now: time.Now, snapshot: &LiveSnapshot{Topics: []LiveTopic{{
		Name: "cancer treatment",
		Growth: StudyGrowth{
			Prior: StudyPeriod{Count: 20, Population: 1000}, Recent: StudyPeriod{Count: 30, Population: 1000},
		},
	}}}}
	board := &LiveMoneyBoard{BuiltAt: time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC),
		Sources: []LiveMoneySource{{Name: "Registry", Records: 16, Events: 16}}}
	for i := range liveMoneyLatest {
		board.Events = append(board.Events, LiveMoneyEvent{ID: strconv.Itoa(i), Kind: LiveMoneyGrant,
			Region: liveMoneyRegionUS, Rubric: "medicine", Date: "2026-10-18"})
	}
	board.Events = append(board.Events,
		LiveMoneyEvent{ID: "wrong-kind", Kind: LiveMoneyGrant, Region: liveMoneyRegionEU, Rubric: "medicine", Date: "2026-10-17"},
		LiveMoneyEvent{ID: "wrong-region", Kind: LiveMoneyRound, Region: liveMoneyRegionUS, Rubric: "energy", Date: "2026-10-16"},
		LiveMoneyEvent{ID: "europe-round", Kind: LiveMoneyRound, Region: liveMoneyRegionEU,
			Rubric: "energy", Date: "2026-10-15", Text: "stored text"},
		LiveMoneyEvent{ID: "unclassified", Kind: LiveMoneyRound, Region: liveMoneyRegionEU, Date: "2026-10-14"},
	)
	s.eventData.money.board = board
	all := s.MoneySummary("", "")
	if all.Total != 16 || len(all.Latest) != liveMoneyLatest || all.Latest[0].ID != "0" || len(all.Science) != 1 {
		t.Fatalf("unfiltered board changed: %+v", all)
	}
	filtered := s.MoneySummary(liveMoneyRegionEU, LiveMoneyRound)
	if filtered.Total != 2 || filtered.Unsorted != 1 || len(filtered.Latest) != 1 ||
		filtered.Latest[0].ID != "europe-round" || filtered.Latest[0].Text != "" {
		t.Fatalf("want the older matching round before limiting latest: %+v", filtered)
	}
	count := 0
	for _, cell := range filtered.Cells {
		if cell.Region != liveMoneyRegionEU || cell.Kind != LiveMoneyRound {
			t.Fatalf("cell outside the combined filter: %+v", cell)
		}
		count += cell.Count
	}
	if count != filtered.Total || !slices.Equal(filtered.Sources, all.Sources) || !slices.Equal(filtered.Science, all.Science) {
		t.Fatal("filtered totals must agree while the source passport and science baseline remain global")
	}
}

func TestLiveMoneyDigestKeepsOnlyEarlyDeals(t *testing.T) {
	t.Parallel()
	feed := liveMoneyFeed{name: "Digest", region: "europe", license: "open"}
	text := "Arkea Bio raised $15m in Series A funding led by Lowercarbon to develop enteric vaccines. " +
		"Hertha Metals raised $134m in Series B to scale low-carbon steel. " +
		"Example Capital closes $50M climate fund to back founders. " +
		"Terra Maps raised $2M seed from Ag Ventures to map soil health for farmers. " +
		"The weather raises questions about the harvest season in the north."
	events := liveMoneyFromRecord(liveMoneyRules(), liveRubrics(), feed,
		"Norwegian startup raises $6.3 million pre-seed round to build AI-native engineering firm",
		text, "https://example.com/digest", "2026-10-12")
	got := map[string]LiveMoneyEvent{}
	for _, event := range events {
		got[event.Quote[:12]] = event
	}
	if len(events) != 3 {
		t.Fatalf("want title, Arkea and Terra deals, got %d: %+v", len(events), events)
	}
	if e := got["Norwegian st"]; e.Stage != liveMoneyStagePreSeed || e.Amount != "$6.3 million" {
		t.Fatalf("title deal: %+v", e)
	}
	if e := got["Arkea Bio ra"]; e.Stage != liveMoneyStageA || e.Company != "Arkea Bio" || e.Investors == "" {
		t.Fatalf("series A deal: %+v", e)
	}
	if e := got["Terra Maps r"]; e.Stage != liveMoneyStageSeed || e.Rubric != "agriculture" || e.Region != "europe" {
		t.Fatalf("seed deal: %+v", e)
	}
}

func TestLiveMoneyMatchesWordStartsOnly(t *testing.T) {
	t.Parallel()
	s := &TrendLiveService{now: time.Now}
	s.eventData.money.board = &LiveMoneyBoard{Events: []LiveMoneyEvent{
		{ID: "a", Kind: LiveMoneyRound, Title: "Solid-state battery maker raises seed", Text: "solid-state batteries"},
		{ID: "b", Kind: LiveMoneyGrant, Title: "Abattery grant", Text: "abattery"},
	}}
	match := s.MoneyMatch([]string{"Battery"})
	if match.Total != 1 || match.Events[0].ID != "a" || match.Events[0].Text != "" {
		t.Fatalf("want only the word-start match without stored text, got %+v", match)
	}
}

func TestLiveMoneyRegionFollowsCountry(t *testing.T) {
	t.Parallel()
	regions := liveMoneyRegions()
	for _, tc := range []struct{ country, fallback, want string }{
		{"DE", liveMoneyUnknown, liveMoneyRegionEU}, {"el", liveMoneyUnknown, liveMoneyRegionEU}, {"UK", "", liveMoneyRegionEU},
		{"IL", liveMoneyRegionEU, liveMoneyRegionME}, {"RU", liveMoneyRegionAS, liveMoneyRegionCIS},
		{"SG", liveMoneyUnknown, liveMoneyRegionAS}, {"", liveMoneyRegionLA, liveMoneyRegionLA},
		{"ZZ", liveMoneyRegionAF, liveMoneyRegionAF},
	} {
		if got := liveMoneyRegion(regions, tc.country, tc.fallback); got != tc.want {
			t.Errorf("region(%q, %q) = %q, want %q", tc.country, tc.fallback, got, tc.want)
		}
	}
	domains := liveMoneyDomainCountry("shop.com.br") + liveMoneyDomainCountry("startup.ai") + liveMoneyDomainCountry("")
	if domains != "BR" {
		t.Errorf("domain country = %q, want BR only", domains)
	}
	// Карта и слой денег красят страну одним регионом: таблица повторяет контур из build.py.
	raw, err := os.ReadFile("../../../../frontend/src/features/trend/worldMap.json")
	if err != nil {
		t.Fatal(err)
	}
	var atlas struct {
		Countries []struct {
			Name   string `json:"n"`
			Code   string `json:"c"`
			Region string `json:"r"`
		} `json:"countries"`
	}
	if err = json.Unmarshal(raw, &atlas); err != nil {
		t.Fatal(err)
	}
	for _, country := range atlas.Countries {
		if got := liveMoneyRegion(regions, country.Code, ""); country.Code != "" && got != country.Region {
			t.Errorf("%s (%s): на карте %s, в слое денег %s", country.Name, country.Code, country.Region, got)
		}
	}
}

func TestLiveMoneyReadsRussianFrenchPortuguese(t *testing.T) {
	t.Parallel()
	rules := liveMoneyRules()
	for _, tc := range []struct{ sentence, company, stage, amount string }{
		{"Defbox привлек первые внешние инвестиции — 10 млн руб. от METASCAN и ФРИИ.", "Defbox", liveMoneyStageUnknown, "10 млн руб"},
		{"Фонд «Восход» вложил 150 млн рублей в стартап «Нейро» при участии бизнес-ангелов", "Нейро", liveMoneyStageSeed,
			"150 млн рублей"},
		{"Ранее Bloomberg сообщал, что OpenAI рассчитывала привлечь $30 млрд при оценке в $1,4 трлн", "", "", ""},
		{"WEALTHCOME lève 15 millions d’euros pour défier Harvest et accélérer auprès des grands comptes", "WEALTHCOME",
			liveMoneyStageUnknown, "15 millions d’euros"},
		{"La startup parisienne MARBLE annonce une Série A de 6,5 millions d’euros menée par SMARTFIN, avec ADNEXUS", "MARBLE",
			liveMoneyStageA, "6,5 millions d’euros"},
		{"La scale-up suédoise Tandem Health boucle une série B de 86,5 millions d’euros menée par le Scaleup Europe Fund", "", "", ""},
		{"A healthtech Falaê recebe aporte de R$ 1,5 milhão em rodada pré-seed liderada pela Bossa Invest", "Falaê",
			liveMoneyStagePreSeed, "R$ 1,5 milhão"},
		{"O montante se soma à rodada Seed de R$ 12 milhões captada em 2025, que teve liderança do fundo Astella",
			"", liveMoneyStageSeed, "R$ 12 milhões"},
		{"Um grande exemplo foi a CloudWalk, que captou R$ 5,5 bilhões por meio de um FIDC, a maior operação", "", "", ""},
		{"A startup pretende captar R$ 10 milhões em rodada seed até dezembro", "", "", ""},
	} {
		event, ok := liveMoneyFromSentence(rules, tc.sentence)
		if ok != (tc.stage != "") || event.Company != tc.company || event.Stage != tc.stage || event.Amount != tc.amount {
			t.Errorf("%q: got ok=%v %q %q %q", tc.sentence, ok, event.Company, event.Stage, event.Amount)
		}
	}
	if usd := liveMoneyAmountUSD("150 млн рублей"); usd != 1875000 {
		t.Fatalf("rubles at 80 per dollar: %v", usd)
	}
}

// Примеры со стенда 19.10: регион, отрасль и заголовок в колонке «Компания», суммы в разных записях и фразы
// вместо инвесторов.
func TestLiveMoneyTidyCleansFeedDeals(t *testing.T) {
	t.Parallel()
	feed := liveMoneyFeeds()[0].name
	events := liveMoneyTidy([]LiveMoneyEvent{
		{ID: "region", Source: feed, Company: "Africa", Amount: "$13.9mn", URL: "u", Investors: "existing investors"},
		{ID: "sector", Source: feed, Company: "Business", URL: "u"},
		{ID: "headline", Source: feed, Company: "EBRD Backs Nigerian Grid-Tech Startup", Amount: "$101.9 million", URL: "u"},
		{ID: "title", Source: feed, Company: "Arnergy Raises Seed", Title: "Arnergy raises seed", URL: "u"},
		{ID: "named", Source: feed, Company: "Arnergy", Amount: "$25 Million", URL: "u",
			Investors: "Partech, with participation from Ventures Platform"},
		{ID: "phrase", Source: feed, Company: "Solvera", Investors: "a group of angel investors from Lagos", URL: "u"},
		{ID: "prefix", Source: feed, Company: "Healthtech Skedia", Amount: "$4M", URL: "u",
			Investors: "Jump Capital and an $8"},
		{ID: "country", Source: feed, Company: "Czech FLÆ Robotics", Amount: "€2M", URL: "u"},
		{ID: "registry", Source: liveMoneyVinnovaSource, Amount: "SEK 500K",
			Company: "FRAUNHOFER GESELLSCHAFT ZUR FORDERUNG DER ANGEWANDTEN FORSCHUNG EV"},
	})
	got := map[string]LiveMoneyEvent{}
	for _, event := range events {
		got[event.ID] = event
	}
	for _, tc := range []struct {
		id, company, currency, investors string
		value                            float64
	}{
		{"region", "", "USD", "", 13.9e6},
		{"headline", "", "USD", "", 101.9e6},
		{"named", "Arnergy", "USD", "Partech, Ventures Platform", 25e6},
		{"prefix", "Skedia", "USD", "Jump Capital", 4e6},
		{"country", "FLÆ Robotics", "EUR", "", 2e6},
		{"phrase", "Solvera", "", "", 0},
		{"registry", "FRAUNHOFER GESELLSCHAFT ZUR FORDERUNG DER ANGEWANDTEN FORSCHUNG EV", "SEK", "", 5e5},
	} {
		e, ok := got[tc.id]
		if !ok || e.Company != tc.company || e.Currency != tc.currency || e.Investors != tc.investors ||
			math.Round(e.AmountValue) != tc.value {
			t.Errorf("%s: kept=%v %+v", tc.id, ok, e)
		}
	}
	if len(events) != 7 {
		t.Errorf("deals left without a company or an amount must go: %+v", events)
	}
	for amount, want := range map[string]struct {
		value    float64
		currency string
	}{
		"10 млн руб": {10e6, "RUB"}, "1,2 млрд рублей": {1.2e9, "RUB"}, "R$ 1,5 milhão": {1.5e6, "BRL"},
		"15 millions d’euros": {15e6, "EUR"}, "NT$2.3M": {2.3e6, "TWD"}, "10 млн": {10e6, ""}, "50억 원": {5e9, "KRW"},
	} {
		if value, currency := liveMoneyAmount(amount); math.Round(value) != want.value || currency != want.currency {
			t.Errorf("amount %q = %v %q, want %v %q", amount, value, currency, want.value, want.currency)
		}
	}
}
