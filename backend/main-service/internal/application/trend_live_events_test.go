package application

import (
	"strings"
	"testing"
)

func TestLiveEventDictionary(t *testing.T) {
	for text, want := range map[string]string{
		"Our policy outperforms baselines on the LIBERO benchmark and is deployed at scale": LiveEventLabResult,
		"Bank of Foo deployed the detector in production across 40 branches":                LiveEventDeployment,
		"We ran a proof of concept deployment at a financial institution":                   LiveEventPrototype,
		"Сбербанк запустил пилот технологии в трёх отделениях":                              LiveEventPilot,
		"Стартап привлёк 300 млн руб. в посевном раунде":                                    LiveEventFunding,
		"Acme open-sourced the runtime; v1.2 is on GitHub":                                  LiveEventCodeRelease,
		"A survey of approaches to anomaly synthesis":                                       "",
	} {
		if got := liveEventTypeOf(text); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
	event := liveEventFromText(LiveAdoptionEvent{Record: LiveSourceRecord{
		Title:   "Mindgard raises $30M Series A for continuous AI red teaming",
		Excerpt: "The round was led by Foo. Mindgard raised $30M Series A to expand continuous AI red teaming.",
	}})
	if event.Type != LiveEventFunding || event.Actor != "Mindgard" || event.Amount != "$30M" || event.Phase != "Series A" {
		t.Fatalf("funding event: %+v", event)
	}
	if liveNewsTypeOf("Мосбиржа начала продажи токенизированных акций") != LiveEventProductLaunch ||
		liveEventTypeOf("Предложенный метод использует графовую нейросеть") != "" {
		t.Fatal("news words apply to market records only")
	}
	grant := LiveAdoptionEvent{Quote: "Phase I: sulfide solid-state batteries for drones"}
	if !liveEventNamed(grant, LiveEventQuery{Term: "solid-state battery"}) {
		t.Fatal("plural forms must still name the technology")
	}
	if !liveEventNamed(event, LiveEventQuery{Term: "continuous ai red teaming"}) ||
		liveEventNamed(event, LiveEventQuery{Term: "prompt injection"}) {
		t.Fatal("event must attach by the technology name only")
	}
}

func TestLiveAssessTZ(t *testing.T) {
	topic := LiveTopic{eventSources: []string{"web", "ietf"}}
	topic.PapersCount = 4
	topic.Growth = StudyGrowth{
		Prior:  StudyPeriod{Period: "2025-09–2026-02", Count: 2, Population: 1000},
		Recent: StudyPeriod{Period: "2026-03–2026-08", Count: 6, Population: 1000},
	}
	liveAssessTZ(&topic, map[string]StudyPaper{})
	searched := "не найдено в 2 рыночных источниках (веб-ленты и Bing News, IETF Datatracker)"
	if topic.TZ.StageCode != 1 || !strings.Contains(topic.TZ.StageBasis, searched) {
		t.Fatalf("papers only: %+v", topic.TZ)
	}
	if topic.TZ.TrendCode != 3 || topic.TZ.Score != 4 || topic.Adoption.Code != liveAdoptionConcept {
		t.Fatalf("trend (6+1)/(2+1): %+v, adoption %+v", topic.TZ, topic.Adoption)
	}
	empty := LiveTopic{Growth: StudyGrowth{Prior: StudyPeriod{Period: "2025-09–2026-02", Population: 1000},
		Recent: StudyPeriod{Period: "2026-03–2026-08", Population: 1000}}}
	empty.PaperIDs = []string{"arxiv:new"}
	if code, _ := liveTZTrend(empty, map[string]StudyPaper{}); code != 1 {
		t.Fatalf("no works in a year must not read as growth, got %d", code)
	}
	if code, _ := liveTZTrend(empty, map[string]StudyPaper{"arxiv:new": {Date: "2026-09-10"}}); code != 2 {
		t.Fatalf("works after the recent half grow from zero, got %d", code)
	}
	topic.Events = []LiveAdoptionEvent{
		{Type: LiveEventDeployment, Source: "web", Quote: "Foo deployed it", Record: LiveSourceRecord{Date: "2026-04-01"}},
		{Type: LiveEventPilot, Source: "sbir", Quote: "Phase II pilot of the detector", Verified: true,
			Record: LiveSourceRecord{Date: "2026-05-01", URL: "https://example.org/award"}},
	}
	liveAssessTZ(&topic, map[string]StudyPaper{})
	if topic.TZ.StageCode != 3 || topic.TZ.StageEvent == nil || topic.TZ.StageEvent.Source != "sbir" ||
		topic.Adoption.Code != liveAdoptionPilot {
		t.Fatalf("unverified deployment must not beat a verified pilot: %+v", topic.TZ)
	}
	topic.Card = &LiveCard{ExampleID: "arxiv:1", Example: "Метод превосходит базовые на LIBERO",
		Evidence: []LiveClaimEvidence{{Field: liveClaimExample, SourceID: "arxiv:1", Verified: true,
			Quote: "Our policy outperforms strong baselines on LIBERO."}}}
	liveAssessTZ(&topic, map[string]StudyPaper{"arxiv:1": {ID: "arxiv:1", Title: "Policy", Date: "2026-06-01"}})
	if topic.Card.ExampleType != LiveEventLabResult || topic.TZ.StageCode != 3 {
		t.Fatalf("benchmark case must stay a lab result: %q, stage %d", topic.Card.ExampleType, topic.TZ.StageCode)
	}
}

func TestLiveEventMaturity(t *testing.T) {
	if liveWikiTechnology("Spear", "Polearm with a long shaft and pointed head") ||
		!liveWikiTechnology("Kubernetes", "Open-source container software") ||
		!liveWikiTechnology("Contactless payment", "") {
		t.Fatal("a one-word article counts only when its description is about technology")
	}
	views := 6816
	topic := LiveTopic{eventSources: []string{"wikipedia", "ietf"}, Events: []LiveAdoptionEvent{
		{Source: liveEventWikipedia, Count: &views, typed: true,
			Record: LiveSourceRecord{Title: "Contactless payment", Date: "2006-09-06", ObservedAt: "2026-09-25T00:00:00Z"}},
	}}
	if level, facts, _ := liveEventMaturity(topic); level != liveMarketForming || len(facts) != 1 {
		t.Fatalf("wikipedia alone: %s %v", level, facts)
	}
	topic.Events = append(topic.Events, LiveAdoptionEvent{Type: LiveEventStandard, Source: liveEventIETF, typed: true,
		Record: LiveSourceRecord{Title: "Contactless Payment Protocol", Date: "2019-01-01", ObservedAt: "2026-09-25T00:00:00Z"}})
	if level, _, _ := liveEventMaturity(LiveTopic{eventSources: topic.eventSources, Events: []LiveAdoptionEvent{
		{Type: LiveEventStandard, Source: liveEventIETF, typed: true,
			Record: LiveSourceRecord{Title: "Post-Quantum Cryptography in OpenPGP", Date: "2026-06-30", ObservedAt: "2026-09-25"}},
	}}); level != liveMarketEarly {
		t.Fatalf("a fresh standard is no sign of a formed market: %s", level)
	}
	market := liveMarketMaturity(topic, liveAbsoluteBar())
	if !market.formed || market.field().Status != liveMarketMature || !strings.HasPrefix(market.reason(), "рынок сформирован") {
		t.Fatalf("two signals: %+v", market.field())
	}
	topic.Market = market.field()
	if kept, rejected := liveAdmitCards([]LiveTopic{topic}, nil); len(kept) != 0 ||
		len(rejected) != 1 || rejected[0].Code != liveRejectMarketFormed {
		t.Fatal("formed market must never be admitted")
	}
}
