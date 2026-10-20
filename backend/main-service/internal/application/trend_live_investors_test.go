package application

import (
	"maps"
	"slices"
	"testing"
	"time"
)

func TestLiveInvestorsFilterFacetsAndDeals(t *testing.T) {
	t.Parallel()
	directory := liveInvestorsBuild("2026-10-18", []LiveInvestor{
		{Name: "Savanna Health Ventures LLC", Type: "vc_manager", Country: "KE", City: "Nairobi",
			Stages: []string{"seed"}, Sectors: []string{"Healthtech"}, NInvestments: 4},
		{Name: "Lagos Angels", Type: "angel_network", Country: "NG", Stages: []string{"angel", "seed"},
			Sectors: []string{"Fintech", "Medical Devices"}, NInvestments: 9},
		{Name: "Cairo Fintech Fund", Type: "vc_fund", Country: "EG", Stages: []string{"series_a"},
			Sectors: []string{"Fintech"}},
		{Name: "Nowhere Capital", Type: "other"},
	})
	board := []LiveMoneyEvent{
		{Kind: LiveMoneyRound, Stage: liveMoneyStageSeed, Company: "Old", Date: "2026-05-01", Rubric: "medicine",
			Investors: "Savanna Health Ventures, Lowercarbon"},
		{Kind: LiveMoneyRound, Stage: liveMoneyStageA, Company: "New", Date: "2026-09-01", Investors: "savanna health ventures"},
		{Kind: LiveMoneyGrant, Stage: LiveMoneyGrant, Date: "2026-09-02", Investors: "Savanna Health Ventures"},
	}
	deals := liveInvestorDealsOf(board, directory.byName)
	for _, tc := range []struct {
		query   LiveInvestorQuery
		names   []string
		regions map[string]int
		rubrics map[string]int
	}{
		{LiveInvestorQuery{}, []string{"Savanna Health Ventures LLC", "Lagos Angels", "Cairo Fintech Fund", "Nowhere Capital"},
			map[string]int{"africa": 2, "mena": 1, "unknown": 1}, map[string]int{"medicine": 2, "fintech": 2}},
		{LiveInvestorQuery{Region: "africa", Rubric: "medicine"}, []string{"Savanna Health Ventures LLC", "Lagos Angels"},
			map[string]int{"africa": 2}, map[string]int{"medicine": 2, "fintech": 1}},
		{LiveInvestorQuery{Rubric: "fintech", Sort: liveInvestorsSortName}, []string{"Cairo Fintech Fund", "Lagos Angels"},
			map[string]int{"africa": 1, "mena": 1}, map[string]int{"medicine": 2, "fintech": 2}},
		{LiveInvestorQuery{Q: "nairobi"}, []string{"Savanna Health Ventures LLC"},
			map[string]int{"africa": 1}, map[string]int{"medicine": 1}},
	} {
		page := directory.page(tc.query, deals, true)
		names := []string{}
		for _, item := range page.Items {
			names = append(names, item.Name)
		}
		if !slices.Equal(names, tc.names) || page.Matched != len(tc.names) || page.Total != 4 {
			t.Fatalf("%+v: got %v of %d", tc.query, names, page.Matched)
		}
		if !maps.Equal(page.Facets.Regions, tc.regions) || !maps.Equal(page.Facets.Rubrics, tc.rubrics) {
			t.Fatalf("%+v: facets %v %v", tc.query, page.Facets.Regions, page.Facets.Rubrics)
		}
	}
	top := directory.page(LiveInvestorQuery{Limit: 1}, deals, true).Items[0]
	if top.Deals.Total != 2 || top.Deals.Latest[0].Company != "New" || top.Deals.Stages[liveMoneyStageSeed] != 1 {
		t.Fatalf("exact-name rounds must link, grants must not: %+v", top.Deals)
	}
	if directory.page(LiveInvestorQuery{}, nil, false).Items[0].Deals != nil {
		t.Fatal("without the money board deals are unknown, not zero")
	}
	if liveInvestorQueryValid(LiveInvestorQuery{Country: "ke"}) || liveInvestorQueryValid(LiveInvestorQuery{Rubric: "x"}) ||
		liveInvestorQueryValid(LiveInvestorQuery{Limit: 101}) {
		t.Fatal("bad filters must be rejected")
	}
}

func TestLiveInvestorsEmbeddedFileParses(t *testing.T) {
	t.Parallel()
	s := &TrendLiveService{now: time.Now}
	page, err := s.Investors(LiveInvestorQuery{})
	if err != nil || page.Status != LiveSearchReady || page.Total < 20000 || len(page.Items) != liveInvestorsPage ||
		page.Facets.Rubrics["medicine"] == 0 || page.Sources[0].Name != liveMoneyFFName {
		t.Fatalf("embedded directory: %v %s total=%d sources=%v", err, page.Status, page.Total, page.Sources[:1])
	}
}
