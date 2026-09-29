package radarcollector

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/main-service/internal/application"
)

func TestSuccessfulSourceRecoveryDoesNotRepeatOnNextTick(t *testing.T) {
	redis := miniredis.RunT(t)
	coordinator := New(redis.Addr(), "")
	t.Cleanup(func() { _ = coordinator.Close() })
	store := &sourceRecoveryStore{}
	live := application.NewTrendLiveService(store, nil)
	live.SetWriterGuard(coordinator.Acquire)
	header := live.Overview()
	observed := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	snapshot := application.LiveSnapshot{
		Version: header.Version, Method: header.Method, ObservedAt: observed, Duration: "1m",
		Periods: []application.LivePeriod{{ID: "2026-09"}},
		Topics:  []application.LiveTopic{{ID: "curated", Query: application.LiveQuery{Terms: []string{"quantum memory"}}}},
		Discovered: []application.LiveTopic{{ID: "discovered",
			Query: application.LiveQuery{Terms: []string{"optical memory"}}}},
	}
	var err error
	store.raw, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/status":
			_ = json.MarshalWrite(w, application.LiveCollectorChannel{
				Status: "ready", ObservedAt: observed, SourceSet: "sources-v1",
			})
		case "/v1/query":
			queries.Add(1)
			_ = json.MarshalWrite(w, application.LiveChannel{
				ObservedAt: observed, SourceSet: "sources-v1", Counts: []*int{new(1)}, Total: 1,
			})
		default:
			t.Errorf("unexpected collector request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	for _, key := range []string{"PATENT_COLLECTOR_URL", "WEB_COLLECTOR_URL", "CN_PATENT_COLLECTOR_URL"} {
		t.Setenv(key, server.URL)
	}
	for range 2 {
		if _, err := tick(t.Context(), live, coordinator, false, false); err != nil {
			t.Fatal(err)
		}
		state, err := coordinator.Status(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if queries.Load() != 6 || len(state.Runs) != 1 || !state.Runs[0].Success || state.Runs[0].Kind != "sources" ||
			state.Phase != idlePhase || redis.Exists(writerKey) {
			t.Fatalf("successful source collection repeated or stayed active: queries=%d, state=%+v", queries.Load(), state)
		}
	}
	for _, topic := range live.Overview().Topics {
		for _, channel := range []*application.LiveChannel{topic.Patents, topic.Web, topic.PatentsCN} {
			if channel == nil || channel.ObservedAt != "" || channel.SourceSet != "" {
				t.Fatal("scheduler metadata leaked into the public topic summary")
			}
		}
	}
}

type sourceRecoveryStore struct {
	mu      sync.Mutex
	raw     []byte
	version int
}

func (s *sourceRecoveryStore) GetBytes(ctx context.Context, key string) ([]byte, error) {
	raw, _, err := s.GetVersion(ctx, key)
	return raw, err
}

func (s *sourceRecoveryStore) GetVersion(context.Context, string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.raw), strconv.Itoa(s.version), nil
}

func (s *sourceRecoveryStore) Put(context.Context, string, io.Reader, int64, string) error {
	return errors.New("unconditional snapshot write")
}

func (s *sourceRecoveryStore) PutIfVersion(_ context.Context, _ string, r io.Reader, _ int64, _ string, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version != strconv.Itoa(s.version) {
		return errors.New("snapshot version conflict")
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.raw = raw
	s.version++
	return nil
}
