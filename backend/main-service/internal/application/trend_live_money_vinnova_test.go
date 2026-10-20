package application

import (
	"encoding/json/v2"
	"testing"
)

func TestLiveMoneyVinnovaKeepsCompanyCoordinators(t *testing.T) {
	t.Parallel()
	raw := `[
{"Diarienummer":"2026-00101","Ärenderubrik":"Smart styrning av plasmaprocesser",
 "ÄrenderubrikEngelska":"Smart control of plasma processes",
 "MalSvenska":"Mål","MalEngelska":"Thin film coating for the semiconductor industry.","BeviljatBidrag":2967945,
 "ProjektStart":"2026-05-01T00:00:00","KoordinatorOrg":"Ionautics AB"},
{"Diarienummer":"2026-00102","Ärenderubrik":"6G","ÄrenderubrikEngelska":"6G IoT","BeviljatBidrag":628832,
 "ProjektStart":"2026-05-01T00:00:00","KoordinatorOrg":"Kungliga Tekniska Högskolan"},
{"Diarienummer":"2026-00103","Ärenderubrik":"Test","ÄrenderubrikEngelska":"Test bed","BeviljatBidrag":100000,
 "ProjektStart":"2026-06-01T00:00:00","KoordinatorOrg":"RISE Research Institutes of Sweden AB"},
{"Diarienummer":"2025-00104","Ärenderubrik":"Gammal","ÄrenderubrikEngelska":"Old","BeviljatBidrag":100000,
 "ProjektStart":"2025-01-01T00:00:00","KoordinatorOrg":"Old AB"}]`
	var projects []liveMoneyVinnovaProject
	if err := json.Unmarshal([]byte(raw), &projects); err != nil {
		t.Fatal(err)
	}
	events, records := liveMoneyVinnovaEvents(liveRubrics(), projects, "2026-04-01", "2026-10-18")
	if records != 3 || len(events) != 1 {
		t.Fatalf("want one company of three projects in window, got %d of %d: %+v", len(events), records, events)
	}
	e := events[0]
	if e.Company != "Ionautics AB" || e.Date != "2026-05-01" || e.Amount != "SEK 3.0M" || e.Country != "SE" ||
		e.Title != "Smart control of plasma processes" || e.Rubric == "" || e.URL != liveMoneyVinnovaURL+"2026-00101" {
		t.Fatalf("vinnova event: %+v", e)
	}
}
