package application

import (
	"strings"
	"testing"
)

func TestLiveConfidenceCountsOnlyMeasuredRules(t *testing.T) {
	empty := liveConfidence(LiveTopic{}, liveAbsoluteBar())
	if empty.Value != 0 || empty.Passed != 0 || len(empty.Missing)+len(empty.Unmeasured) != empty.Total {
		t.Fatalf("пустая тема получила уверенность: %+v", empty)
	}
	if empty.AboveMin {
		t.Error("пустая тема выше порога")
	}
	if empty.Basis == "" || empty.Method == "" {
		t.Error("уверенность без основания и метода")
	}
}

func TestLiveConfidenceRisesWithEvidence(t *testing.T) {
	topic := LiveTopic{
		StudyTopic: StudyTopic{
			Stage: liveStageEmerging, Momentum: liveMomentumUp, FirstYear: "2026",
			ActiveYears: 3, Groups: 4, PaperIDs: []string{"p1", "p2"},
			External:      []StudyEvent{{URL: "https://github.com/o/r"}},
			TopGroupShare: new(0.25),
		},
		Web: &LiveChannel{Records: []LiveSourceRecord{{URL: "https://nplus1.ru/a"}}},
		Market: &LiveMarket{Status: liveMarketEarly, Known: true, Basis: "Первый прототип новой технологии",
			Evidence: []LiveClaimEvidence{{Field: liveClaimMarket, SourceID: "p1",
				Quote: "The first prototype of this technology", Verified: true}}},
		Emergence: &LiveEmergence{Class: liveEmergenceWeak},
	}
	got := liveConfidence(topic, liveAbsoluteBar())
	if got.Passed != got.Total {
		t.Fatalf("полностью подтверждённая тема не прошла все правила: %+v", got)
	}
	if !got.AboveMin || got.Value != 1 {
		t.Errorf("уверенность %v, выше порога %v", got.Value, got.AboveMin)
	}
}

func TestLiveConfidenceDropsForEstablished(t *testing.T) {
	got := liveConfidence(LiveTopic{StudyTopic: StudyTopic{
		Stage: liveStageEstablished, Momentum: liveMomentumUp, ActiveYears: 5,
		Groups: 9, PaperIDs: []string{"p"}, FirstYear: "2019",
	}}, liveAbsoluteBar())
	if got.AboveMin {
		t.Fatalf("сложившееся направление выше порога: %+v", got)
	}
	if len(got.Missing) == 0 {
		t.Error("не названо ни одной непройденной проверки")
	}
}

func TestLiveConfidenceDoesNotRewardUnmeasuredRules(t *testing.T) {
	got := liveConfidence(LiveTopic{StudyTopic: StudyTopic{
		Stage: liveStageEmerging, Momentum: liveMomentumUp, FirstYear: "2026",
		ActiveYears: 3, Groups: 4, PaperIDs: []string{"p1"},
		External: []StudyEvent{{URL: "https://github.com/o/r"}},
	}}, liveAbsoluteBar())
	if len(got.Unmeasured) != 3 || got.Total != 8 || got.Measured != 5 {
		t.Fatalf("неизмеренные правила изменили общий знаменатель: %+v", got)
	}
	if got.Value != 5.0/8 || got.AboveMin {
		t.Fatalf("неизвестность рынка, концентрации или карты повысила долю: %+v", got)
	}
	if !strings.Contains(got.Basis, "3 правил не измерено") {
		t.Errorf("основание не называет число неизмеренных правил: %q", got.Basis)
	}
}

func TestLiveConfidenceSeparatesSingleGroupDirection(t *testing.T) {
	topic := LiveTopic{StudyTopic: StudyTopic{
		Stage: liveStageEmerging, Momentum: liveMomentumUp, FirstYear: "2026",
		ActiveYears: 3, Groups: 4, PaperIDs: []string{"p1", "p2"},
		External:      []StudyEvent{{URL: "https://github.com/o/r"}},
		TopGroupShare: new(0.25),
	}}
	spread := liveConfidence(topic, liveAbsoluteBar())
	topic.TopGroupShare = new(0.9)
	single := liveConfidence(topic, liveAbsoluteBar())
	if spread.Value <= single.Value {
		t.Fatalf("одна группа на всё направление не понизила долю: %v против %v", single.Value, spread.Value)
	}
	if single.Total != spread.Total {
		t.Errorf("знаменатель поехал: %d против %d", single.Total, spread.Total)
	}
}

func TestCountersCountConfidentDirections(t *testing.T) {
	confident := LiveTopic{Confidence: &LiveConfidence{Value: 0.86, AboveMin: true}}
	if got := liveCounters(confident, nil, nil, 0); got.AboveThreshold != 1 {
		t.Fatalf("уверенное направление не посчитано: %+v", got)
	}
	weak := LiveTopic{Confidence: &LiveConfidence{Value: 0.4}}
	if got := liveCounters(weak, nil, nil, 0); got.AboveThreshold != 0 {
		t.Fatalf("неуверенное направление посчитано: %+v", got)
	}
}
