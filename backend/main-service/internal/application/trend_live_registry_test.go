package application

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const registryPage = `{"totalCount":2,"studies":[
{"protocolSection":{"identificationModule":{"nctId":"NCT01","briefTitle":"mRNA  vaccine  study"},
 "statusModule":{"overallStatus":"RECRUITING","startDateStruct":{"date":"2026-08-14"}},
 "designModule":{"phases":["PHASE1","PHASE2"]},
 "sponsorCollaboratorsModule":{"leadSponsor":{"name":"BioNTech SE"}}}},
{"protocolSection":{"identificationModule":{"nctId":"NCT02","briefTitle":"Stopped trial"},
 "statusModule":{"overallStatus":"TERMINATED","startDateStruct":{"date":"2026-09"}},
 "designModule":{"phases":["PHASE3"]},
 "sponsorCollaboratorsModule":{"leadSponsor":{"name":"Universiteit Antwerpen"}}}}
]}`

func registrySources(t *testing.T, body string, status int) (*liveSources, string, *string) {
	t.Helper()
	query := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return &liveSources{http: server.Client()}, server.URL, &query
}

func TestRegistryMapsStudies(t *testing.T) {
	src, base, raw := registrySources(t, registryPage, http.StatusOK)
	channel := liveRegistryAt(t.Context(), src, []string{"mRNA vaccine"}, scholarPeriods(), base)
	if channel == nil {
		t.Fatal("канал не собран")
	}
	if !strings.Contains(*raw, "StartDate") {
		t.Errorf("запрос без фильтра по дате: %s", *raw)
	}
	if len(channel.Records) != 2 {
		t.Fatalf("записей %d, ожидали 2: %+v", len(channel.Records), channel.Records)
	}
	first := channel.Records[0]
	if first.Title != "mRNA vaccine study" || first.URL != "https://clinicaltrials.gov/study/NCT01" {
		t.Errorf("запись разобрана неверно: %+v", first)
	}
	if first.Trust != TrustHigh || first.PublisherType != PublisherRegistry {
		t.Errorf("государственный реестр обязан быть доверенным: %+v", first)
	}
	if first.Language != "en" || first.LanguageBasis == "" {
		t.Errorf("язык записи не проставлен: %+v", first)
	}
	if !strings.Contains(first.TextStatus, "вторая фаза") {
		t.Errorf("стадия из фазы не взята: %q", first.TextStatus)
	}
	if channel.Records[1].Date != "2026-09-01" {
		t.Errorf("месячная дата не приведена к дню: %q", channel.Records[1].Date)
	}
	if !strings.Contains(channel.Records[1].TextStatus, "остановлено") {
		t.Errorf("остановленное испытание не отмечено: %q", channel.Records[1].TextStatus)
	}
	total := 0
	for _, count := range channel.Counts {
		if count != nil {
			total += *count
		}
	}
	if total != 2 {
		t.Errorf("счётчики периодов: %d вместо 2", total)
	}
}

func TestRegistryFailureBecomesLimit(t *testing.T) {
	src, base, _ := registrySources(t, "", http.StatusServiceUnavailable)
	channel := liveRegistryAt(t.Context(), src, []string{"mRNA vaccine"}, scholarPeriods(), base)
	if channel == nil || len(channel.Limits) == 0 {
		t.Fatalf("ожидали ограничение, получили %+v", channel)
	}
	if len(channel.Records) != 0 {
		t.Errorf("при отказе записей быть не должно: %+v", channel.Records)
	}
}

// Запрос без латинских слов не идёт в реестр: он отвечает только по-английски.
func TestRegistrySkipsQueryWithoutLatin(t *testing.T) {
	src, base, raw := registrySources(t, registryPage, http.StatusOK)
	channel := liveRegistryAt(t.Context(), src, []string{"электролиз"}, scholarPeriods(), base)
	if *raw != "" {
		t.Errorf("запрос ушёл в реестр: %s", *raw)
	}
	if channel == nil || len(channel.Limits) == 0 {
		t.Fatal("ожидали названное ограничение")
	}
}

func TestRegistryStageNaming(t *testing.T) {
	if got := liveRegistryStage(nil, "RECRUITING"); got != "фаза не указана" {
		t.Errorf("без фазы: %q", got)
	}
	if got := liveRegistryStage([]string{"EARLY_PHASE1"}, "WITHDRAWN"); !strings.Contains(got, "остановлено") {
		t.Errorf("отозванное испытание: %q", got)
	}
}

func TestLiveRegistryOnTopicDropsOffTopicRecords(t *testing.T) {
	t.Parallel()
	periods := []LivePeriod{
		{Start: "2026-01-01", EndExclusive: "2026-02-01"},
		{Start: "2026-02-01", EndExclusive: "2026-03-01"},
	}
	topic := LiveTopic{
		Query: LiveQuery{Terms: []string{"Quantum Information"}},
		Registry: &LiveChannel{
			Total: 3, Partial: true,
			Counts: []*int{new(2), new(1)},
			Records: []LiveSourceRecord{
				{ID: "a", Title: "Colorectal Cancer Screening", Date: "2026-01-10"},
				{ID: "b", Title: "Direct Discrimination of Quantum Information States", Date: "2026-02-05"},
				{ID: "c", Title: "Fall Risk Among Older Adults", Date: "2026-01-20"},
			},
		},
	}
	got := liveRegistryOnTopic(topic, periods)
	if len(got.Registry.Records) != 1 || got.Registry.Records[0].ID != "b" {
		t.Fatalf("оставлены не те записи: %+v", got.Registry.Records)
	}
	if got.Registry.Total != 1 || got.Registry.Partial {
		t.Fatalf("итог не пересчитан: total=%d partial=%v", got.Registry.Total, got.Registry.Partial)
	}
	if *got.Registry.Counts[0] != 0 || *got.Registry.Counts[1] != 1 {
		t.Fatalf("ряд по месяцам не пересчитан: %d %d", *got.Registry.Counts[0], *got.Registry.Counts[1])
	}
	if len(got.Registry.Limits) != 1 || !strings.Contains(got.Registry.Limits[0], "отброшено 2") {
		t.Fatalf("оговорка не названа: %v", got.Registry.Limits)
	}
	if topic.Registry.Total != 3 {
		t.Fatalf("исходный снимок изменён: %d", topic.Registry.Total)
	}
}
