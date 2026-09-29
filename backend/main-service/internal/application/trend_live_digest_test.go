package application

import (
	"context"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

type digestSequenceGenerator struct {
	fakeGenerator
	responses []aiclients.GenResponse
	requests  []aiclients.GenRequest
}

func (g *digestSequenceGenerator) Generate(_ context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	g.requests = append(g.requests, request)
	return g.responses[min(len(g.requests)-1, len(g.responses)-1)], nil
}

func TestSourceDigestPublishesOnlyCompletedRussianSummary(t *testing.T) {
	const completed = "Исследование выявило коллапс предсказаний при оценке аудио."
	for _, tc := range []struct {
		name    string
		first   aiclients.GenResponse
		recover bool
		calls   int
	}{
		{"completed", aiclients.GenResponse{Text: completed, FinishReason: "stop"}, true, 1},
		{"token_limit_with_punctuation", aiclients.GenResponse{Text: completed, FinishReason: "length"}, true, 2},
		{"missing_finish_reason", aiclients.GenResponse{Text: "Аудио-оценка показала коллапс предсказаний и"}, true, 2},
		{"mixed_language", aiclients.GenResponse{Text: "Based на результатах исследования выявлены ограничения."}, true, 2},
		{"still_incomplete", aiclients.GenResponse{Text: "Исследование показало", FinishReason: "length"}, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.first
			first.Model = "digest-model"
			second := aiclients.GenResponse{Text: completed, Model: "digest-model", FinishReason: "stop"}
			if !tc.recover {
				second = first
			}
			gen := &digestSequenceGenerator{responses: []aiclients.GenResponse{first, second}}
			digest, model, err := NewSourceDigest(gen, func(context.Context) bool { return true })(
				t.Context(), "Speech benchmark", "Audio evaluation reveals prediction collapse.")
			if len(gen.requests) != tc.calls || (err == nil) != tc.recover {
				t.Fatalf("requests=%d, error=%v", len(gen.requests), err)
			}
			if tc.recover && (digest != completed || model != "digest-model") {
				t.Fatalf("invalid summary: digest=%q model=%q", digest, model)
			}
			if !tc.recover && (digest != "" || model != "") {
				t.Fatalf("unfinished summary leaked: %q", digest)
			}
			for _, request := range gen.requests {
				if request.MaxTokens < 300 || request.MaxTokens > 512 {
					t.Fatalf("summary budget is inadequate or unbounded: %d", request.MaxTokens)
				}
			}
		})
	}
}
