package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/example/main-service/internal/platform/openalex"
)

func TestHistoryCacheRetriesFailedPersistedRead(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		err       error
		retry     bool
	}{
		{name: "deadline", err: fmt.Errorf("get object: %w", context.DeadlineExceeded), retry: true},
		{name: "invalid_json", raw: `{"partial":{"2025":1},"broken":`, retry: true},
		{name: "access_denied", err: &smithy.GenericAPIError{Code: "AccessDenied"}, retry: true},
		{name: "no_such_key", err: fmt.Errorf("get object: %w", &smithy.GenericAPIError{Code: "NoSuchKey"})},
		{name: "not_found", err: fmt.Errorf("get object: %w", &smithy.GenericAPIError{Code: "NotFound"})},
		{name: "empty_cache", raw: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads, writes, requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			client, err := openalex.New(openalex.Config{BaseURL: server.URL, Attempts: 1})
			if err != nil {
				t.Fatal(err)
			}
			cache := newLiveHistoryCache(client)
			const month = "2026-09"
			saved := `{"persisted":{"2025":12},"fresh":{"2025":1}}`
			cache.store = historyReadTestStore{
				get: func(_ context.Context, key string) ([]byte, error) {
					if key != liveHistoryObject(month) {
						t.Errorf("unexpected cache object: %s", key)
					}
					if reads.Add(1) == 1 {
						return []byte(tc.raw), tc.err
					}
					return []byte(saved), nil
				},
				put: func(reader io.Reader) error {
					raw, readErr := io.ReadAll(reader)
					if readErr != nil {
						return fmt.Errorf("read history fixture: %w", readErr)
					}
					saved = string(raw)
					writes.Add(1)
					return nil
				},
			}
			cache.load(t.Context(), month)
			if _, partial := cache.known[month+"|partial"]; partial {
				t.Fatal("invalid JSON published a partially decoded history")
			}
			cache.known[month+"|fresh"] = LiveTermHistory{2025: 99}
			cache.save(t.Context(), month)
			if tc.retry && writes.Load() != 0 {
				t.Fatal("fresh in-memory history overwrote the unread persisted month")
			}
			phrases, expectedReads := []string{"fresh"}, int32(1)
			if tc.retry {
				phrases, expectedReads = append(phrases, "persisted"), 2
			}
			found := cache.histories(t.Context(), phrases, month)
			if found["fresh"][2025] != 99 || tc.retry && found["persisted"][2025] != 12 ||
				reads.Load() != expectedReads || requests.Load() != 0 {
				t.Fatalf("cache recovery lost persisted/fresh values or queried OpenAlex: found=%v reads=%d requests=%d",
					found, reads.Load(), requests.Load())
			}
			before := writes.Load()
			cache.save(t.Context(), month)
			var persisted map[string]LiveTermHistory
			if err := json.Unmarshal([]byte(saved), &persisted); err != nil {
				t.Fatal(err)
			}
			if writes.Load() != before+1 || persisted["fresh"][2025] != 99 || tc.retry && persisted["persisted"][2025] != 12 {
				t.Fatalf("recovered cache was not saved with merged history: %v", persisted)
			}
			var workers sync.WaitGroup
			for range 4 {
				workers.Go(func() { _ = cache.histories(t.Context(), phrases, month) })
			}
			workers.Wait()
			if reads.Load() != expectedReads || requests.Load() != 0 {
				t.Fatalf("successful or missing cache was reloaded: reads=%d requests=%d", reads.Load(), requests.Load())
			}
		})
	}
}

type historyReadTestStore struct {
	get func(context.Context, string) ([]byte, error)
	put func(io.Reader) error
}

func (s historyReadTestStore) GetBytes(ctx context.Context, key string) ([]byte, error) {
	return s.get(ctx, key)
}

func (s historyReadTestStore) Put(_ context.Context, _ string, reader io.Reader, _ int64, _ string) error {
	return s.put(reader)
}
