package application

import (
	"context"
	"fmt"
	"strconv"
	"testing"
)

func TestLiveExecutiveWriterKeepsOnlyValidFields(t *testing.T) {
	t.Parallel()
	detail := LiveDetail{RunID: "run", ObservedAt: "2026-09-23T00:00:00Z",
		Counters: LiveCounters{SourcesProcessed: 120, Candidates: 40}}
	for i := range 7 {
		detail.Candidates = append(detail.Candidates, LiveTopic{ID: strconv.Itoa(i),
			Name: fmt.Sprint("технология ", i), Card: &LiveCard{Summary: "Описание технологии"}})
	}
	reply := `{"items":[{"id":"0","benefit":"Антифрод: раньше замечает мошеннические переводы","horizon":"1–3",
		"step":"pilot","reason":"Есть кейс внедрения в платёжной системе"},
		{"id":"1","benefit":"Useful for payments","horizon":"soon","step":"maybe","reason":""},
		{"id":"чужой","benefit":"Платежи: лишняя строка","horizon":"0-1","step":"watch"}]}`
	on := func(context.Context) bool { return true }
	brief, err := NewLiveExecutiveWriter(fakeGenerator{reply}, on)(t.Context(), "финтех", detail)
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.Items) != liveExecutiveTop || brief.Selected != 7 || brief.Sources != 120 || brief.Candidates != 40 {
		t.Fatalf("шапка или число строк: %+v", brief)
	}
	first, second := brief.Items[0], brief.Items[1]
	if first.Horizon != "1-3" || first.Step != "pilot" || first.Benefit == "" || first.Summary != "Описание технологии" {
		t.Fatalf("годная строка потеряна: %+v", first)
	}
	if second.Benefit != "" || second.Horizon != "" || second.Step != "" {
		t.Fatalf("негодные поля приняты: %+v", second)
	}
}
