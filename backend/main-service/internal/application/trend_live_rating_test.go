package application

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

func TestMaturityRating(t *testing.T) {
	gen := &qualificationGenerator{replies: []aiclients.GenResponse{
		{Text: "**stage**: 3\n**failed_or_faded**: true\n**confidence**: 0.7"},
		{Text: "I cannot rate this technology."},
		{Text: `{"stage": 1, "failed_or_faded": false, "known_since": null, "confidence": 0.4}`},
	}}
	rater := NewLiveMaturityRater(gen, func(context.Context) bool { return true })
	evidence := LiveRatingEvidence{Total: 2, Titles: []string{
		LiveRatingTitle(strings.Repeat("ж", 170), "2026-09-04"), LiveRatingTitle("Older work", "2026-01-30")}}

	rating, err := rater.Rate(t.Context(), "phase coherence", evidence)
	if err != nil || *rating != (LiveMaturityRating{Stage: 3, Failed: true, Confidence: 0.7}) {
		t.Fatalf("ответ списком не разобран: %+v %v", rating, err)
	}
	request := gen.requests[0]
	wantQuery := "Technology: phase coherence\nEvidence: 2 matching papers in the window.\n- " +
		strings.Repeat("ж", 160) + " (2026-09)\n- Older work (2026-01)"
	if request.Query != wantQuery || request.System != liveRatingSystem || request.MaxTokens != 80 ||
		request.Temperature != 0 || !request.JSONMode {
		t.Fatalf("не та подсказка: %+v", request)
	}
	if _, err = rater.Rate(t.Context(), "phase coherence", evidence); err != nil || len(gen.requests) != 1 {
		t.Fatalf("тот же вход спрошен снова: %d вызовов, %v", len(gen.requests), err)
	}

	evidence.Total = 3
	if rating, err = rater.Rate(t.Context(), "phase coherence", evidence); err == nil || rating != nil {
		t.Fatalf("ответ без стадии принят: %+v", rating)
	}

	rating, err = rater.Rate(t.Context(), "phase coherence", LiveRatingEvidence{})
	if err != nil || !strings.HasSuffix(gen.requests[2].Query, "\nEvidence: 0 matching papers in the window.\n(no titles)") {
		t.Fatalf("окно без работ: %q %v", gen.requests[2].Query, err)
	}
	if inputs := LiveRatingInputs(rating); inputs[0] != 1 || inputs[1] != 0 || inputs[2] != 0.4 {
		t.Fatalf("входы: %v", inputs)
	}
	if inputs := LiveRatingInputs(nil); len(inputs) != 3 || !math.IsNaN(inputs[1]) {
		t.Fatalf("без оценки входы неизмерены: %v", inputs)
	}
}

// «Дефицитное орошение» 17.10: ответ без уверенности давал стадии 4 из 4 оценку 0,99 вместо зрелой.
func TestMaturityRatingWithoutConfidence(t *testing.T) {
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	for _, answer := range []string{`{"stage": 4, "failed_or_faded": false, "confidence": null}`,
		`{"stage": 4, "failed_or_faded": false, "known_since": 19`,
		`{"stage": 4, "failed_or_faded": false, "known_since": 1989, "confidence": 0`} {
		rating, ok := liveReadRating(answer)
		evidence := map[string]*WeakSignalEvidence{"deficit irrigation": {Rating: &rating}}
		got := model.prediction("x", []string{"deficit irrigation"}, evidence, "2026-09")
		if !ok || !math.IsNaN(LiveRatingInputs(&rating)[2]) || got.Label != liveClassifierMature {
			t.Fatalf("%s: уверенность %v, класс %s, оценка %.3f", answer, rating.Confidence, got.Label, got.Value)
		}
	}
	// Явный ноль модель не видела: вход прижат к краю обучения, и сигнал на нём не выдаётся.
	zero := map[string]*WeakSignalEvidence{"deficit irrigation": {Rating: &LiveMaturityRating{Stage: 1}}}
	if got := model.prediction("x", []string{"deficit irrigation"}, zero, "2026-09"); got.Label != liveClassifierInsufficient ||
		!strings.Contains(got.Basis, "вне диапазона обучающей выборки") {
		t.Fatalf("выброс входа не помечен: %s %q", got.Label, got.Basis)
	}
}
