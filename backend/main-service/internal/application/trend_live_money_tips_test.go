package application

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

func TestLiveMoneyTIPSKeepsSelectionYearInWindow(t *testing.T) {
	t.Parallel()
	raw := `{"code":200,"data":[
{"id":"a2bb7f62","companyType":"T","name":"히츠","ceo":"김우연","industry12Codes":"[\"BIO\"]","industryStdCodes":"[\"BIO_MED\"]",
 "regionCd":"11","estDt":"2020-05-18","intro":"히츠는 저분자의약품 분야에서 기술 개발을 전개하는 기업입니다. 인공지능 플랫폼.",
 "selYear":"2026","tipsTracks":"[\"GENERAL\"]","operatorName":"슈미트","bizAgeCode":"LT7"},
{"id":"19d545d9","companyType":"T","name":"히트솔","ceo":"박병수","industry12Codes":"[\"ETC\"]","industryStdCodes":"[\"MECH_MAT\"]",
 "intro":"열처리기술","selYear":"2024","operatorName":"스케일업파트너스"}],"total":4588,"page":1,"size":100}`
	var page liveMoneyTIPSPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	events := liveMoneyTIPSEvents(liveRubrics(), page, "2026-04-01", "2026-10-18")
	if len(events) != 1 {
		t.Fatalf("want only the 2026 selection, got %+v", events)
	}
	e := events[0]
	if e.Company != "히츠" || e.Date != "2026-01-01" || e.Investors != "슈미트" || e.Rubric != "biotechnology" ||
		e.Country != "KR" || e.Kind != LiveMoneyAccelerator || !strings.HasSuffix(e.URL, "companyId=a2bb7f62") ||
		e.Quote != "히츠는 저분자의약품 분야에서 기술 개발을 전개하는 기업입니다" || strings.Contains(e.Text, "김우연") {
		t.Fatalf("tips event: %+v", e)
	}
}
