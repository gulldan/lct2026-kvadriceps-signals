package application

import (
	"encoding/json/v2"
	"testing"
)

func TestLiveMoneyNIHForeignMapsRePORTERCountry(t *testing.T) {
	t.Parallel()
	var row liveMoneyNIHProject
	raw := `{"appl_id":11651667,"project_title":"Malaria vaccine trial","abstract_text":"Phase II trial.",
"award_amount":50000,"award_notice_date":"2026-09-28T00:00:00","activity_code":"U01","agency_code":"NIH",
"project_detail_url":"https://reporter.nih.gov/project-details/11651667",
"organization":{"org_name":"IFAKARA HEALTH INSTITUTE","org_country":"TANZANIA U REP"}}`
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	e := liveMoneyNIHForeignEvent(liveRubrics(), liveMoneyNIHCountries(), row)
	if e.Kind != LiveMoneyResearch || e.Country != "TZ" || e.Date != "2026-09-28" || e.Amount != "$50K" ||
		e.Investors != "NIH, U01" || e.Company != "IFAKARA HEALTH INSTITUTE" || e.Rubric != "medicine" {
		t.Fatalf("nih foreign event: %+v", e)
	}
}
