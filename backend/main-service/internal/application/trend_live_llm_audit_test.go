package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

type liveAuditTestGenerator struct {
	request  aiclients.GenRequest
	response aiclients.GenResponse
	err      error
}

func (g *liveAuditTestGenerator) Generate(_ context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	g.request = request
	return g.response, g.err
}

func (g *liveAuditTestGenerator) Stream(ctx context.Context, request aiclients.GenRequest,
	onToken func(string) error,
) (aiclients.GenResponse, error) {
	response, err := g.Generate(ctx, request)
	if err == nil {
		if callbackErr := onToken(response.Text); callbackErr != nil {
			return response, fmt.Errorf("test stream callback: %w", callbackErr)
		}
	}
	return response, err
}

// Обе формы вызова и отказ: данные и текст ошибки провайдера не выходят в журнал.
func TestLiveLLMAuditPreservesProvenanceWithoutContent(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx := context.WithValue(t.Context(), liveLLMRunKey{}, "run-provenance-example")
	providerErr := errors.New("provider body with Bearer credential-private")
	for _, tc := range []struct {
		name, system, operation string
		stream, measured        bool
		err                     error
	}{
		{"generate_usage", liveCardSystem, "write_card", false, true, nil},
		{"stream_unknown_usage", liveDigestSystem, "interpret_source", true, false, nil},
		{"generate_failure", "system-private", "unclassified", false, false, providerErr},
		{"stream_failure", liveCardVerifySystem, "verify_card", true, false, providerErr},
		{"scoped_extract", liveExtractSystem + liveQueryScopeInstruction, "extract_candidates", false, true, nil},
		{"scoped_write", liveCardSystem + liveQueryScopeInstruction, "write_card", false, true, nil},
		{"scoped_verify", liveCardVerifySystem + liveQueryScopeInstruction, "verify_card", true, false, nil},
		{
			"unknown_variant", liveCardSystem + liveQueryScopeInstruction + "\nunknown instruction",
			"unclassified", false, false, nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			request := aiclients.GenRequest{System: tc.system, Query: "query-private", Context: "source-private"}
			response := aiclients.GenResponse{Text: "output-private", Model: "test-model-v1"}
			if tc.measured {
				response.Usage = aiclients.Usage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16}
			}
			if tc.err != nil {
				response.Model = ""
			}
			upstream := &liveAuditTestGenerator{response: response, err: tc.err}
			generator := NewLiveAuditedGenerator(upstream, func(context.Context) string { return "configured-model-v2" })
			var got aiclients.GenResponse
			var err error
			var streamed string
			if tc.stream {
				got, err = generator.Stream(ctx, request, func(token string) error { streamed += token; return nil })
			} else {
				got, err = generator.Generate(ctx, request)
			}
			if got != response || upstream.request != request || !errors.Is(err, tc.err) {
				t.Fatal("audit changed the upstream request, response or error identity")
			}
			if tc.stream && tc.err == nil && streamed != response.Text {
				t.Fatal("audit did not forward stream tokens")
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatalf("expected one JSON audit record: %v", err)
			}
			for key, want := range map[string]any{
				"msg": "radar_llm_call", "run_id": "run-provenance-example", "operation": tc.operation,
				"requested_model": "configured-model-v2",
				"model":           response.Model, "succeeded": tc.err == nil, "usage_reported": tc.measured,
				"prompt_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tc.system))),
			} {
				if record[key] != want {
					t.Fatalf("audit field %s = %v; want %v", key, record[key], want)
				}
			}
			if duration, ok := record["duration_ms"].(float64); !ok || duration < 0 || record["time"] == nil {
				t.Fatal("audit lacks valid timing")
			}
			for key, want := range map[string]float64{"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16} {
				value, exists := record[key]
				if exists != tc.measured || tc.measured && value != want {
					t.Fatalf("audit field %s does not distinguish unknown usage from measured tokens", key)
				}
			}
			for _, private := range []string{request.System, request.Query, request.Context, response.Text, providerErr.Error()} {
				if strings.Contains(output.String(), private) {
					t.Fatal("private content leaked into the audit log")
				}
			}
			if _, exists := record["cost_usd"]; exists {
				t.Fatal("audit inferred a price without reported cost")
			}
		})
	}
}
