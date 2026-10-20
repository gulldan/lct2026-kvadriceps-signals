package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

type conditionalTestStore struct {
	sync.Mutex
	raw     []byte
	version string
}

func (s *conditionalTestStore) GetBytes(ctx context.Context, key string) ([]byte, error) {
	raw, _, err := s.GetVersion(ctx, key)
	return raw, err
}
func (s *conditionalTestStore) GetVersion(context.Context, string) ([]byte, string, error) {
	s.Lock()
	defer s.Unlock()
	return append([]byte(nil), s.raw...), s.version, nil
}
func (s *conditionalTestStore) Put(context.Context, string, io.Reader, int64, string) error {
	return errors.New("unconditional write forbidden")
}
func (s *conditionalTestStore) PutIfVersion(_ context.Context, _ string, r io.Reader, _ int64, _ string, v string) error {
	s.Lock()
	defer s.Unlock()
	if s.version != v {
		return errors.New("conflict")
	}
	var err error
	s.raw, err = io.ReadAll(r)
	s.version = string(s.raw)
	return err
}

func TestConcurrentSnapshotPublicationKeepsWinner(t *testing.T) {
	store := &conditionalTestStore{}
	a, b := NewTrendLiveService(store, nil), NewTrendLiveService(store, nil)
	ca, fa, err := a.beginWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer fa()
	cb, fb, err := b.beginWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer fb()
	if err := b.publish(cb, &LiveSnapshot{ObservedAt: "winner"}); err != nil {
		t.Fatal(err)
	}
	if err := a.publish(ca, &LiveSnapshot{ObservedAt: "stale"}); err == nil {
		t.Fatal("stale writer replaced winner")
	}
	raw, _, _ := store.GetVersion(t.Context(), "")
	if !strings.Contains(string(raw), "winner") || a.snapshot != nil {
		t.Fatal("conflict did not preserve last good observation")
	}
}

func TestCancelledPublicationDoesNotWrite(t *testing.T) {
	store := &conditionalTestStore{}
	svc := NewTrendLiveService(store, nil)
	ctx, finish, err := svc.beginWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	claimed := store.version
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := svc.publish(ctx, &LiveSnapshot{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if store.version != claimed {
		t.Fatal("cancelled job published")
	}
	if _, _, err := svc.beginWrite(t.Context()); !errors.Is(err, ErrLiveBusy) {
		t.Fatal("local sources/full guard missing")
	}
}

func TestPublishedSnapshotDropsSourcePageLog(t *testing.T) {
	store := &conditionalTestStore{}
	svc := NewTrendLiveService(store, nil)
	ctx, finish, err := svc.beginWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	patents := &LiveChannel{Records: []LiveSourceRecord{{ID: "record:1", URL: "https://patents.example/1"}},
		Coverage: []LiveSourceCoverage{{Source: "patent", NextCursor: "page-7",
			Pages: []LiveSourceProvenance{{URL: "https://patents.example/page/1"}}}}}
	topics := []LiveTopic{{Name: "Аддитивное производство", Patents: patents}}
	if err := svc.publish(ctx, &LiveSnapshot{Version: liveVersion, Method: liveMethod, Topics: topics}); err != nil {
		t.Fatal(err)
	}
	raw, _, _ := store.GetVersion(t.Context(), "")
	if strings.Contains(string(raw), "patents.example/page/1") {
		t.Fatal("page log of the source is copied into every direction of the snapshot")
	}
	if !strings.Contains(string(raw), "page-7") || !strings.Contains(string(raw), "patents.example/1") {
		t.Fatalf("coverage cursor or records lost: %s", raw)
	}
	if len(patents.Coverage[0].Pages) != 1 {
		t.Fatal("publication changed the caller's channel")
	}
}
