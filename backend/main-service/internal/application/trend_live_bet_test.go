package application

import (
	"strings"
	"testing"
	"time"
)

func TestLiveBetObserveWritesRealChanges(t *testing.T) {
	before := LiveTopic{Name: "anomaly synthesis", TZ: &LiveTZAssessment{StageCode: 1, TrendCode: 1, Score: 2}}
	baseline := liveBetSnapshotOf(before, "2026-09-24T00:00:00Z")
	bet := LiveBet{Horizon: "3+", Indicators: liveBetIndicators(baseline), Baseline: &baseline, Snapshot: &baseline,
		Rest: []byte(`{"decision":"watch"}`)}
	pilot := LiveAdoptionEvent{Type: LiveEventPilot, Source: "web", Verified: true, Actor: "Bank X",
		Quote: "Bank X piloted anomaly synthesis", Record: LiveSourceRecord{URL: "https://example.test/pilot"}}
	after := LiveTopic{Name: "anomaly synthesis", Events: []LiveAdoptionEvent{pilot, {Type: LiveEventPilot,
		Source: "web", Record: LiveSourceRecord{URL: "https://example.test/unverified"}}},
		TZ: &LiveTZAssessment{StageCode: 3, TrendCode: 3, Score: 6, StageEvent: &pilot, TrendBasis: "за полгода вдвое"}}
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)

	changes := liveBetObserve(&bet, after, "2026-09-25T00:00:00Z", map[string]bool{}, now)
	var lines strings.Builder
	for _, change := range changes {
		lines.WriteString(change.Text + "|" + change.Quote + "\n")
	}
	text := lines.String()
	for _, want := range []string{"Появилось событие «пилот»: Bank X|Bank X piloted",
		"Стадия по шкале ТЗ сменилась: концепция или исследование → пилот",
		"Поток упоминаний перешёл в быстрый рост. за полгода вдвое", "Сработал индикатор: стадия по шкале ТЗ не ниже 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("history misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "unverified") || bet.Status != liveBetConfirming || len(bet.History) != len(changes) {
		t.Fatalf("status %q, history %d, changes:\n%s", bet.Status, len(bet.History), text)
	}
	if again := liveBetObserve(&bet, after, "2026-09-26T00:00:00Z", map[string]bool{pilot.Record.URL: true},
		now.Add(24*time.Hour)); len(again) != 0 {
		t.Fatalf("same answer produced changes: %+v", again)
	}

	// Прогноз по ответу без стадии и тренда: первое измерение — точка отсчёта, а не сработавший порог.
	blank := liveBetSnapshotOf(LiveTopic{Name: "anomaly synthesis"}, "2026-09-24T00:00:00Z")
	early := LiveBet{Indicators: liveBetIndicators(blank), Baseline: &blank, Snapshot: &blank}
	for _, change := range liveBetObserve(&early, after, "2026-09-25T00:00:00Z", map[string]bool{}, now) {
		if strings.HasPrefix(change.Text, "Сработал индикатор") {
			t.Fatalf("first measurement fired an indicator: %s", change.Text)
		}
	}
	if early.Status != liveBetUnchanged || early.Baseline.StageCode != 3 {
		t.Fatalf("first measurement must become the baseline: %q %+v", early.Status, early.Baseline)
	}

	detail, err := liveBetWrite(`{"problem_addressed":"p","radar":{"query":"q","sources":[1]}}`, bet)
	if err != nil {
		t.Fatal(err)
	}
	radar, ok := liveBetOf(detail)
	if !ok || !strings.Contains(detail, `"problem_addressed":"p"`) || !strings.Contains(detail, `"sources":[1]`) ||
		!strings.Contains(detail, `"decision":"watch"`) || radar.Bet.Status != liveBetConfirming {
		t.Fatalf("detail lost fields: %s", detail)
	}
}

func TestLiveSignalGroundsSkipsRepeats(t *testing.T) {
	findings := "Findings: accurate motion."
	abstract := "Method on Walker2. " + findings
	grounds := liveSignalGrounds(LiveSignalBriefInput{
		Candidate: LiveTopic{
			TZ: &LiveTZAssessment{StageCode: 3,
				StageBasis: "стадия 3 — пилот: утверждение карточки проверено по цитате: робот собрал заказ"},
			Card: &LiveCard{Evidence: []LiveClaimEvidence{
				{SourceID: "a", Quote: abstract, Verified: true},
				{SourceID: "a", Quote: findings, Verified: true},
				{SourceID: "a", Quote: abstract, Verified: true},
			}},
			Events: []LiveAdoptionEvent{{Type: LiveEventPilot, Verified: true, Quote: abstract}},
		},
		Sources: []LiveSignalSource{{ID: "a", Title: "H-WBC"}},
	})
	want := "Стадия по шкале ТЗ: 3 — пилот. робот собрал заказ\n«Findings: accurate motion.» — H-WBC"
	if grounds != want {
		t.Fatalf("grounds:\n%s", grounds)
	}
}
