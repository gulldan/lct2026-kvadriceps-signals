package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type responseFunc func(*http.Request) (*http.Response, error)

func (f responseFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTemporarySourceFailureRetriesWithinTheSameBudgetAndLimiter(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovers", true: "stops_after_three"}[persistent], func(t *testing.T) {
			now := testObserved()
			f := newFetcher(3 * time.Second)
			f.now = func() time.Time { return now }
			f.wait = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
			var starts []time.Time
			f.http.Transport = responseFunc(func(r *http.Request) (*http.Response, error) {
				starts = append(starts, now)
				if persistent || len(starts) == 1 {
					return nil, &url.Error{Op: "Get", URL: "https://source.invalid/?secret=do-not-log", Err: io.EOF}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
					Body: io.NopCloser(strings.NewReader("exact primary bytes")), Request: r}, nil
			})
			raw, _, err := f.get(t.Context(), "https://source.invalid/primary")
			if persistent {
				if !errors.Is(err, errTemporaryTransport) || len(starts) != 3 || len(raw) != 0 ||
					strings.Contains(err.Error(), "do-not-log") {
					t.Fatalf("failure exceeded the budget, advanced data, or leaked its URL: %v", err)
				}
			} else if err != nil || string(raw) != "exact primary bytes" || len(starts) != 2 {
				t.Fatalf("temporary EOF prevented a bounded retry: %v", err)
			}
			for i := 1; i < len(starts); i++ {
				if starts[i].Sub(starts[i-1]) < 3*time.Second {
					t.Fatal("network retry bypassed the provider limiter")
				}
			}
		})
	}
}

func TestGatewayFailureRetriesBeforeAdvancingSourcePage(t *testing.T) {
	f := newFetcher(time.Second)
	now := testObserved()
	f.now = func() time.Time { return now }
	f.wait = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	calls := 0
	f.http.Transport = responseFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		status, body := http.StatusBadGateway, "temporary proxy failure"
		if calls == 2 {
			status, body = http.StatusOK, "exact source response"
		}
		return &http.Response{StatusCode: status, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	raw, _, err := f.get(t.Context(), "https://source.invalid/page")
	if err != nil || calls != 2 || string(raw) != "exact source response" {
		t.Fatalf("temporary gateway failure prevented source collection: calls=%d error=%v", calls, err)
	}
}
