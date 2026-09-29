package application

import (
	"context"
	"testing"
)

func TestLiveEventCheckRequiresGroundedMechanism(t *testing.T) {
	const quote = "The detector uses inertial micro-motion signals to distinguish living users from replay attacks."
	identity := LiveCardIdentity{Label: "Проверка живости", Mechanism: "Анализ инерциальных микродвижений"}
	events := []LiveAdoptionEvent{{Type: LiveEventPilot, Quote: quote}}
	for _, tc := range []struct {
		name     string
		response string
		accepted bool
	}{
		{"direct mechanism", `{"events":[{"n":1,"about":true,"type":"pilot",
			"reason":"Подтверждён анализ инерциальных микродвижений.",
			"mechanism_quote":"uses inertial micro-motion signals to distinguish living users"}]}`, true},
		{"bare approval", `{"events":[{"n":1,"about":true,"type":"pilot"}]}`, false},
		{"invented quote", `{"events":[{"n":1,"about":true,"type":"pilot",
			"reason":"Есть совпадение.",
			"mechanism_quote":"The detector analyzes a single RGB camera image."}]}`, false},
		{"missing explanation", `{"events":[{"n":1,"about":true,"type":"pilot",
			"mechanism_quote":"uses inertial micro-motion signals to distinguish living users"}]}`, false},
		{"conflicting verdicts", `{"events":[{"n":1,"about":false,"type":"none"},{"n":1,"about":true,"type":"pilot",
			"reason":"Есть совпадение.",
			"mechanism_quote":"uses inertial micro-motion signals to distinguish living users"}]}`, false},
		{"unknown type", `{"events":[{"n":1,"about":true,"type":"commercial_success",
			"reason":"Есть совпадение.",
			"mechanism_quote":"uses inertial micro-motion signals to distinguish living users"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := NewLiveEventCheck(fakeGenerator{text: tc.response}, func(context.Context) bool { return true })
			verdicts, err := check(t.Context(), identity, events)
			if err != nil || len(verdicts) != 1 {
				t.Fatalf("check failed: %v, %+v", err, verdicts)
			}
			got := liveApplyEventVerdicts(events, []int{0}, verdicts)
			if (len(got) == 1 && got[0].Verified) != tc.accepted {
				t.Fatalf("event admission = %+v, want accepted=%v", got, tc.accepted)
			}
		})
	}
}
