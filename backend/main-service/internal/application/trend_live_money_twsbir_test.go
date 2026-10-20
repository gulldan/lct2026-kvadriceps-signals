package application

import "testing"

func TestLiveMoneyTWSBIRReadsApprovalsInWindow(t *testing.T) {
	t.Parallel()
	raw := "\ufeff序號,獎補助事項,獎補助對象類別,統一編號,獎補助對象,獎補助對象所在縣市別代碼,獎補助對象所在直轄市或縣市名稱,核准日期,補助日期,補助金額(元)\n" +
		"1,運用國產AI晶片之眼鏡開發計畫,企業,80308207,宏芯科技股份有限公司,,新北市,20260817,,1300000\n" +
		"2,舊計畫,企業,1,舊公司,,臺北市,20250105,,900000\n"
	events, records, err := liveMoneyTWSBIREvents(liveRubrics(), []byte(raw), "2026-04-01", "2026-10-19")
	if err != nil || records != 1 || len(events) != 1 {
		t.Fatalf("want one approval in window: %+v, %d, %v", events, records, err)
	}
	if e := events[0]; e.Company != "宏芯科技股份有限公司" || e.Date != "2026-08-17" || e.Amount != "NT$1.3M" ||
		e.Country != "TW" || e.Quote != "運用國產AI晶片之眼鏡開發計畫" {
		t.Fatalf("tw sbir event: %+v", e)
	}
}
