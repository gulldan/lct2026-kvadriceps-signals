package aiclients_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/vlm-service/internal/platform/aiclients"
)

type generationTransport func(*http.Request) (*http.Response, error)

func (f generationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGenerationRetriesTransientFailuresWithinBudget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		persist bool
		calls   int
	}{
		{"gateway_recovers", 502, false, 2},
		{"overload_recovers", 503, false, 2},
		{"gateway_timeout_recovers", 504, false, 2},
		{"rate_limit_recovers", 429, false, 2},
		{"gateway_remains_down", 502, true, 3},
		{"invalid_request", 400, false, 1},
		{"invalid_key", 401, false, 1},
		{"forbidden", 403, false, 1},
		{"unknown_model", 404, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				client := &http.Client{Transport: generationTransport(func(*http.Request) (*http.Response, error) {
					calls++
					status, body := http.StatusOK, `{"choices":[{"message":{"content":"Справка готова."},"finish_reason":"stop"}]}`
					if calls == 1 || tc.persist {
						status, body = tc.status, `{"error":{"message":"upstream failure"}}`
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
				})}
				generator := aiclients.NewGenerator("http://gateway.test", "model", "", 0, client)
				started := time.Now()
				out, err := generator.Generate(t.Context(), aiclients.GenRequest{Query: "Составь справку"})
				if calls != tc.calls || (err == nil) != (tc.calls == 2) {
					t.Fatalf("calls=%d, error=%v; want calls=%d", calls, err, tc.calls)
				}
				if err == nil && (out.Text != "Справка готова." || out.FinishReason != "stop") {
					t.Fatalf("response metadata lost: %+v", out)
				}
				if elapsed := time.Since(started); elapsed > 5*time.Second || tc.calls == 1 && elapsed != 0 {
					t.Fatalf("retry budget exceeded: %v", elapsed)
				}
			})
		})
	}

	t.Run("deadline_cancels_backoff", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: generationTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
			})}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			generator := aiclients.NewGenerator("http://gateway.test", "model", "", 0, client)
			_, err := generator.Generate(ctx, aiclients.GenRequest{Query: "Составь справку"})
			if calls != 1 || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancellation ignored: calls=%d error=%v", calls, err)
			}
		})
	})
}
