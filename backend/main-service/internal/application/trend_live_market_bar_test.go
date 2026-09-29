package application

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLiveMarketBarScalesToUmbrella(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	web := make([]LiveSourceRecord, 0, 3)
	for i := range 3 {
		web = append(web, LiveSourceRecord{ID: fmt.Sprintf("w%d", i), Title: "Digital Ruble Settlement",
			Date: "2026-07-01", URL: fmt.Sprintf("https://media-%d.example/news", i), Source: "Медиа"})
	}
	patents := []LiveSourceRecord{
		{ID: "p1", Title: "Settlement", Date: "2024-01-01", Priority: "2024-01-01", URL: "https://p.example/1"},
		{ID: "p2", Title: "Settlement", Date: "2025-01-01", Priority: "2025-01-01", URL: "https://p.example/2"},
	}
	umbrella := LiveTopic{StudyTopic: StudyTopic{Series: liveSeries(periods, nil)},
		Web:     &LiveChannel{Source: "web", Total: len(web), Records: web},
		Patents: &LiveChannel{Source: "patents", Total: len(patents), Records: patents}}
	candidate := umbrella
	candidate.Web = &LiveChannel{Source: "web", Total: 2, Records: web[:2]}

	if market := liveMarketMaturity(candidate, liveAbsoluteBar()); market.mature {
		t.Fatalf("абсолютный порог не должен набираться на тонком сборе: %+v", market)
	}
	market := liveMarketMaturity(candidate, liveMarketBarOf(umbrella))
	if market.known || market.mature {
		t.Fatalf("адаптивный порог выдан за доказательство зрелости: %+v", market)
	}
	if field := market.field(); field.Status != liveMarketUnknown || len(field.Facts) < 2 {
		t.Fatalf("диагностика корпуса пропала или названа рыночным вердиктом: %+v", field)
	}
}

func TestLiveMarketNamesUnmetThresholds(t *testing.T) {
	topic := LiveTopic{Web: &LiveChannel{Source: "web", Total: 1, Records: []LiveSourceRecord{
		{ID: "w1", Title: "Solid State Batteries", Date: "2026-07-01", URL: "https://one.example/a"},
	}}}
	field := liveMarketMaturity(topic, liveAbsoluteBar()).field()
	if field.Mature || field.Known {
		t.Fatalf("одна запись не измеряет зрелость рынка: %+v", field)
	}
	if !strings.Contains(strings.Join(field.Checks, "; "), "издателей 1 при пороге 6") {
		t.Fatalf("ненабранный порог не назван числом: %+v", field.Checks)
	}
}
