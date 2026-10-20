package application

import (
	"encoding/json/v2"
	"testing"
)

func TestLiveMoneyIATIPlacesDevelopmentMoney(t *testing.T) {
	t.Parallel()
	var activities liveMoneyIATIRowsOf[liveMoneyIATIActivity]
	var countries liveMoneyIATIRowsOf[liveMoneyIATICountry]
	var sectors liveMoneyIATIRowsOf[liveMoneyIATISector]
	for raw, out := range map[string]any{
		`{"rows":[
{"aid":"DAC-1601-INV-1","reporting_ref":"DAC-1601","title":"Malaria vaccine trial","description":"Phase II.",
 "sum_trans_usd":1500000,"max_trans_day":20738},
{"aid":"44000-P1","reporting_ref":"44000","title":"Rural roads","description":null,"sum_trans_usd":9e7,"max_trans_day":20700},
{"aid":"XM-DAC-903-SII-1","reporting_ref":"XM-DAC-903","title":"Solar lender","description":"","sum_trans_usd":2.5e6,
 "max_trans_day":20629},
{"aid":"XM-DAC-903-SII-2","reporting_ref":"XM-DAC-903","title":"Ukraine bank","sum_trans_usd":1e6,
 "max_trans_day":20629}]}`: &activities,
		`{"rows":[{"aid":"DAC-1601-INV-1","country_code":"KE","max_country_percent":60},
{"aid":"DAC-1601-INV-1","country_code":"US","max_country_percent":40},
{"aid":"44000-P1","country_code":"ZM","max_country_percent":100},
{"aid":"XM-DAC-903-SII-1","country_code":"ET","max_country_percent":100},
{"aid":"XM-DAC-903-SII-2","country_code":"UA","max_country_percent":100}]}`: &countries,
		`{"rows":[{"aid":"DAC-1601-INV-1","sector_code":"12250"},{"aid":"44000-P1","sector_code":"21020"}]}`: &sectors,
	} {
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			t.Fatal(err)
		}
	}
	events := liveMoneyIATIEvents(liveRubrics(), liveMoneyIATIPublishers(), activities.Rows, countries.Rows, sectors.Rows)
	if len(events) != 2 {
		t.Fatalf("want the health grant and the IFC loan without sectors, not roads or Ukraine: %+v", events)
	}
	gates, ifc := events[0], events[1]
	if gates.Kind != LiveMoneyDevelopment || gates.Country != "KE" || gates.Region != liveMoneyRegionAF ||
		gates.Date != "2026-10-12" || gates.Amount != "$1.5M" || gates.Investors != "Gates Foundation" ||
		gates.License != "ODC-BY, Gates Foundation" || gates.URL != liveMoneyIATICard+"DAC-1601-INV-1" {
		t.Fatalf("gates event: %+v", gates)
	}
	if ifc.Investors != "IFC" || ifc.Country != "ET" || ifc.Date != "2026-06-25" {
		t.Fatalf("ifc event: %+v", ifc)
	}
}
