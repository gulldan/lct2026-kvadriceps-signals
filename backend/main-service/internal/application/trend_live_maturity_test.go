package application

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Ранняя стадия в науке при многолетнем патентном потоке и десятке издателей — сложившийся рынок.
func TestLiveRejectNamesMarketFacts(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	years := []string{"2016", "2017", "2019", "2021"}
	patents := make([]LiveSourceRecord, 0, len(years))
	for i, year := range years {
		patents = append(patents, LiveSourceRecord{ID: fmt.Sprintf("pat-%d", i),
			Title: "Container Orchestration Scheduler", Date: year + "-04-01", Priority: year + "-04-01",
			URL: "https://patents.example/" + year, Source: "EPO OPS"})
	}
	web := make([]LiveSourceRecord, 0, 8)
	for i := range 8 {
		web = append(web, LiveSourceRecord{ID: fmt.Sprintf("web-%d", i),
			Title: "Container Orchestration in Production", Date: "2026-07-01",
			URL: fmt.Sprintf("https://vendor-%d.example/blog/post", i), Source: fmt.Sprintf("Вендор %d", i)})
	}
	topic := LiveTopic{
		StudyTopic:  StudyTopic{Series: liveSeries(periods, nil), Stage: liveStageEarly},
		PapersCount: 4, DocumentsCount: 12,
		Patents: &LiveChannel{Source: "patents", Total: len(patents), Records: patents},
		Web:     &LiveChannel{Source: "web", Total: len(web), Records: web},
	}
	if market := liveMarketMaturity(topic, liveAbsoluteBar()); market.known || market.mature {
		t.Fatalf("счётчики публикаций без явного свидетельства объявлены рынком: %+v", market)
	}
	topic.Market = &LiveMarket{Status: liveMarketMature, Known: true, Mature: true,
		Basis: "источник описывает стандарт оркестрации с массовым применением",
		Evidence: []LiveClaimEvidence{{Field: liveClaimMarket, SourceID: "web-0",
			Quote: "The standard orchestration technology is used in production at scale", Verified: true}}}
	market := liveMarketMaturity(topic, liveAbsoluteBar())
	if !market.known || !market.mature {
		t.Fatalf("сложившийся рынок не распознан: %+v", market)
	}
	code, reason := liveRejectReason(topic, liveEvidenceTiers()[0], liveAbsoluteBar())
	if code != liveRejectMature {
		t.Fatalf("рыночная зрелость отклонена не по своему основанию: %q %q", code, reason)
	}
	if !strings.Contains(reason, "стандарт оркестрации") {
		t.Fatalf("причина не называет источник зрелости: %q", reason)
	}
	if len(market.facts) < 3 {
		t.Fatalf("диагностические счётчики исчезли: %+v", market)
	}
}

func TestLiveMaturityStaysUnknownOnSilentChannels(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	papers := make([]StudyPaper, 0, 6)
	for i := range 6 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:y%d", i),
			Title: "Agent Identity Fabric", Date: periods[len(periods)-2].Start,
			Authors: []string{fmt.Sprintf("Lab %d", i)}})
	}
	young := LiveTopic{StudyTopic: StudyTopic{Series: liveSeries(periods, papers), Stage: liveStageEarly},
		PapersCount: len(papers), DocumentsCount: len(papers)}
	if market := liveMarketMaturity(young, liveAbsoluteBar()); market.known || market.mature {
		t.Fatalf("пустые каналы посчитаны измерением рынка: %+v", market)
	}
	young.Web = &LiveChannel{Records: []LiveSourceRecord{{Title: "A recent announcement", URL: "https://vendor.example/new"}}}
	if market := liveMarketMaturity(young, liveAbsoluteBar()).field(); market.Known || market.Status != liveMarketUnknown ||
		strings.Contains(market.Basis, "рынок ещё не сложился") {
		t.Fatalf("один материал стал доказательством раннего рынка: %+v", market)
	}
	if code, reason := liveRejectReason(young, liveEvidenceTiers()[0], liveAbsoluteBar()); code != "" {
		t.Fatalf("молодая технология отклонена без рыночных данных: %q %q", code, reason)
	}
}

func TestLiveHistoryRejectsLongFlatAndKeepsGrowing(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 10000
	}
	flat := liveHistoryTopic(t, periods, []int{5, 5, 5, 5, 5, 5, 5, 1, 1, 1, 1, 1, 1, 0})
	age := liveScienceAge(flat)
	if !age.long {
		t.Fatalf("давнее ровное направление не распознано: стадия %q, месяцев %d, импульс %q",
			flat.Stage, flat.ActiveYears, flat.Momentum)
	}
	code, reason := liveRejectReason(flat, liveEvidenceTiers()[0], liveAbsoluteBar())
	if code != liveRejectHistory {
		t.Fatalf("давнее направление не отклонено по своему основанию: %q %q", code, reason)
	}
	for _, fact := range []string{"1-й месяц окна из 14", "активных месяцев 13 из 14",
		"доля в потоке не растёт", "зрелость рынка не проверена"} {
		if !strings.Contains(reason, fact) {
			t.Fatalf("причина не называет факт %q: %q", fact, reason)
		}
	}
	// Старые корни при растущей доле — искомый сигнал: одна дата первой работы отказом быть не может.
	rising := liveHistoryTopic(t, periods, []int{4, 1, 1, 1, 1, 1, 1, 8, 8, 8, 8, 8, 8, 2})
	if rising.Stage != liveStageEstablished || rising.Momentum != liveMomentumUp {
		t.Fatalf("проверка бессмысленна: стадия %q, импульс %q", rising.Stage, rising.Momentum)
	}
	if code, reason := liveRejectReason(rising, liveEvidenceTiers()[0], liveAbsoluteBar()); code != "" {
		t.Fatalf("растущее направление отклонено по возрасту первой работы: %q %q", code, reason)
	}
}

func liveHistoryTopic(t *testing.T, periods []LivePeriod, perMonth []int) LiveTopic {
	t.Helper()
	total := 0
	for _, count := range perMonth {
		total += count
	}
	papers := make([]StudyPaper, 0, total)
	for i, count := range perMonth {
		for range count {
			papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:h%d", len(papers)),
				Title: "Supply Chain Risk Assessment", Date: periods[i].Start,
				Authors: []string{fmt.Sprintf("Lab %d", len(papers)%5)}})
		}
	}
	topic := LiveTopic{StudyTopic: StudyTopic{Series: liveSeries(periods, papers)},
		PapersCount: len(papers), DocumentsCount: len(papers)}
	liveAssess(&topic, periods, papers)
	return topic
}
