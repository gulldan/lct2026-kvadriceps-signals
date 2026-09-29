package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/example/email-parser/internal/application"
	"github.com/example/email-parser/internal/platform/contracts"

	commonv1 "github.com/example/email-parser/internal/platform/genproto/common/v1"
)

var errBoom = errors.New("boom")

type fakeExtractor struct {
	text string
	err  error
}

func (f *fakeExtractor) Extract(_ context.Context, _ []byte) (string, error) {
	return f.text, f.err
}

type fakeStore struct {
	data    []byte
	getErr  error
	putErr  error
	putKey  string
	putBody string
	putHit  bool
}

func (f *fakeStore) GetBytes(_ context.Context, _ string) ([]byte, error) {
	return f.data, f.getErr
}

func (f *fakeStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	f.putHit = true
	f.putKey = key
	b, _ := io.ReadAll(r)
	f.putBody = string(b)
	return f.putErr
}

type fakeStatus struct {
	statuses []string
	err      error
}

func (f *fakeStatus) UpdateDocumentStatus(_ context.Context, _, status, _ string, _ *int32) error {
	f.statuses = append(f.statuses, status)
	return f.err
}

type publishCall struct {
	exchange   string
	routingKey string
	msg        proto.Message
}

type fakePublisher struct {
	calls     []publishCall
	failOnKey string
	err       error
}

func (f *fakePublisher) PublishProto(_ context.Context, exchange, routingKey string, msg proto.Message) error {
	f.calls = append(f.calls, publishCall{exchange: exchange, routingKey: routingKey, msg: msg})
	if f.err != nil && routingKey == f.failOnKey {
		return f.err
	}
	return nil
}

func sampleEvent() *commonv1.DocumentUploaded {
	return &commonv1.DocumentUploaded{
		DocumentId: "doc-1", OwnerId: "owner-1", ObjectKey: "raw/doc-1.eml",
		Filename: "mail.eml", MimeType: "message/rfc822", TraceId: "trace-1",
	}
}

func chunkPayload(t *testing.T, pub *fakePublisher) *commonv1.DocumentParsed {
	t.Helper()

	for _, c := range pub.calls {
		if c.routingKey != contracts.RouteChunk {
			continue
		}
		parsed, ok := c.msg.(*commonv1.DocumentParsed)
		if !ok {
			t.Fatalf("chunk message is %T, want *DocumentParsed", c.msg)
		}
		return parsed
	}
	return nil
}

func TestProcessInlineSuccess(t *testing.T) {
	t.Parallel()

	store := &fakeStore{data: []byte("raw bytes")}
	status := &fakeStatus{}
	pub := &fakePublisher{}
	p := application.New(&fakeExtractor{text: "extracted body"}, store, status, pub)

	if err := p.Process(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	parsed := chunkPayload(t, pub)
	if parsed == nil {
		t.Fatalf("no DocumentParsed published to chunk route")
	}
	if parsed.GetText() != "extracted body" {
		t.Errorf("inline text = %q, want %q", parsed.GetText(), "extracted body")
	}
	if parsed.GetTextObjectKey() != "" {
		t.Errorf("text_object_key should be empty for inline text, got %q", parsed.GetTextObjectKey())
	}
	if parsed.GetSource() != "email" {
		t.Errorf("source = %q, want email", parsed.GetSource())
	}
	if parsed.GetDocumentId() != "doc-1" || parsed.GetOwnerId() != "owner-1" || parsed.GetTraceId() != "trace-1" {
		t.Errorf("identity fields not propagated: %+v", parsed)
	}
	if store.putHit {
		t.Errorf("Put should not be called for inline text")
	}
	assertStatuses(t, status, []string{contracts.StatusParsing, contracts.StatusParsed})
}

func TestProcessOffloadLargeText(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", (4<<20)+1)
	store := &fakeStore{data: []byte("raw")}
	status := &fakeStatus{}
	pub := &fakePublisher{}
	p := application.New(&fakeExtractor{text: big}, store, status, pub)

	if err := p.Process(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if !store.putHit {
		t.Fatalf("Put was not called for oversized text")
	}
	if store.putKey != contracts.ParsedObjectKey("doc-1", big) {
		t.Errorf("offload key = %q, want parsed/doc-1.txt", store.putKey)
	}
	if store.putBody != big {
		t.Errorf("offloaded body length = %d, want %d", len(store.putBody), len(big))
	}
	parsed := chunkPayload(t, pub)
	if parsed == nil {
		t.Fatalf("no DocumentParsed published")
	}
	if parsed.GetText() != "" {
		t.Errorf("inline text should be cleared after offload, got %d chars", len(parsed.GetText()))
	}
	if parsed.GetTextObjectKey() != contracts.ParsedObjectKey("doc-1", big) {
		t.Errorf("text_object_key = %q, want parsed/doc-1.txt", parsed.GetTextObjectKey())
	}
}

func TestProcessErrors(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", (4<<20)+1)

	tests := []struct {
		name      string
		store     *fakeStore
		extractor *fakeExtractor
		pub       *fakePublisher
		want      string
	}{
		{
			name:      "download fails",
			store:     &fakeStore{getErr: errBoom},
			extractor: &fakeExtractor{text: "x"},
			pub:       &fakePublisher{},
			want:      "download",
		},
		{
			name:      "extract fails",
			store:     &fakeStore{data: []byte("raw")},
			extractor: &fakeExtractor{err: errBoom},
			pub:       &fakePublisher{},
			want:      "extract text",
		},
		{
			name:      "store parsed text fails",
			store:     &fakeStore{data: []byte("raw"), putErr: errBoom},
			extractor: &fakeExtractor{text: big},
			pub:       &fakePublisher{},
			want:      "store parsed text",
		},
		{
			name:      "publish parsed fails",
			store:     &fakeStore{data: []byte("raw")},
			extractor: &fakeExtractor{text: "small"},
			pub:       &fakePublisher{failOnKey: contracts.RouteChunk, err: errBoom},
			want:      "publish parsed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status := &fakeStatus{}
			p := application.New(tt.extractor, tt.store, status, tt.pub)

			err := p.Process(context.Background(), sampleEvent())
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			for _, st := range status.statuses {
				if st == contracts.StatusFailed {
					t.Fatalf("first attempt must not mark failed; saw %v", status.statuses)
				}
			}

			redelivered := contracts.WithAttempt(context.Background(), contracts.MaxAttempts-1)
			if err := p.Process(redelivered, sampleEvent()); err == nil {
				t.Fatalf("expected error on redelivery, got nil")
			}
			if got := status.statuses[len(status.statuses)-1]; got != contracts.StatusFailed {
				t.Errorf("final status = %q, want failed", got)
			}
		})
	}
}

func TestProcessStatusUpdateError(t *testing.T) {
	t.Parallel()

	store := &fakeStore{data: []byte("raw")}
	status := &fakeStatus{err: errBoom}
	pub := &fakePublisher{}
	p := application.New(&fakeExtractor{text: "body"}, store, status, pub)

	if err := p.Process(context.Background(), sampleEvent()); !errors.Is(err, contracts.ErrDurability) {
		t.Fatalf("Process must preserve delivery for durable retry, got: %v", err)
	}
}

func TestProcessEmitError(t *testing.T) {
	t.Parallel()

	store := &fakeStore{data: []byte("raw")}
	status := &fakeStatus{}
	pub := &fakePublisher{failOnKey: "", err: errBoom} // пустой ключ маршрутизации — рассылка событий
	p := application.New(&fakeExtractor{text: "body"}, store, status, pub)

	if err := p.Process(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Process should ignore emit errors, got: %v", err)
	}

	var sawEvents bool
	for _, c := range pub.calls {
		if c.exchange == contracts.ExchangeEvents {
			sawEvents = true
			if _, ok := c.msg.(*commonv1.IngestionEvent); !ok {
				t.Errorf("events message is %T, want *IngestionEvent", c.msg)
			}
		}
	}
	if !sawEvents {
		t.Errorf("expected at least one events publish")
	}
}

func assertStatuses(t *testing.T, status *fakeStatus, want []string) {
	t.Helper()

	if len(status.statuses) != len(want) {
		t.Fatalf("status transitions = %v, want %v", status.statuses, want)
	}
	for i, w := range want {
		if status.statuses[i] != w {
			t.Fatalf("status[%d] = %q, want %q", i, status.statuses[i], w)
		}
	}
}
