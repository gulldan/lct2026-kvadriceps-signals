package ingestion

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/example/pdf-parser/internal/platform/contracts"

	commonv1 "github.com/example/pdf-parser/internal/platform/genproto/common/v1"
)

type deliveryRecorder struct {
	t      *testing.T
	fail   string
	err    error
	ops    []string
	stored string
	key    string
	event  *commonv1.IngestionEvent
	parsed *commonv1.DocumentParsed
}

func (r *deliveryRecorder) record(ctx context.Context, operation string) error {
	r.t.Helper()
	if contracts.DocumentGeneration(ctx) != 7 {
		r.t.Fatal("document generation was lost")
	}
	r.ops = append(r.ops, operation)
	if r.fail == operation {
		return r.err
	}
	return nil
}

func (r *deliveryRecorder) UpdateDocumentStatus(ctx context.Context, id, status, _ string, count *int32) error {
	r.t.Helper()
	if id != "doc" || count != nil {
		r.t.Fatal("status changed document identity or chunk count")
	}
	return r.record(ctx, status)
}

func (r *deliveryRecorder) PublishProto(ctx context.Context, exchange, route string, msg proto.Message) error {
	r.t.Helper()
	switch message := msg.(type) {
	case *commonv1.IngestionEvent:
		if exchange != contracts.ExchangeEvents || route != "" {
			r.t.Fatal("event sent to the wrong exchange")
		}
		r.event = message
		return r.record(ctx, "event")
	case *commonv1.DocumentParsed:
		if exchange != contracts.ExchangeIngestion || route != contracts.RouteChunk {
			r.t.Fatal("parsed document sent to the wrong queue")
		}
		r.parsed = message
		return r.record(ctx, "publish")
	default:
		r.t.Fatalf("unexpected message %T", msg)
		return nil
	}
}

func (r *deliveryRecorder) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	r.t.Helper()
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size || contentType != "text/plain; charset=utf-8" {
		r.t.Fatalf("invalid stored text: size=%d type=%q err=%v", size, contentType, err)
	}
	r.stored, r.key = string(data), key
	return r.record(ctx, "store")
}

func TestReporterPreservesDurabilityAndRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, fail string
		attempt    int
		wantOps    []string
	}{
		{name: "retry", attempt: 0},
		{name: "final", attempt: contracts.MaxAttempts - 1, wantOps: []string{contracts.StatusFailed, "event"}},
		{name: "database unavailable", attempt: contracts.MaxAttempts - 1, fail: contracts.StatusFailed,
			wantOps: []string{contracts.StatusFailed}},
		{name: "notification unavailable", attempt: contracts.MaxAttempts - 1, fail: "event",
			wantOps: []string{contracts.StatusFailed, "event"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New("extraction failed")
			recorder := &deliveryRecorder{t: t, fail: tc.fail, err: errors.New("dependency unavailable")}
			ctx := contracts.WithAttempt(contracts.WithDocumentGeneration(t.Context(), 7), tc.attempt)
			err := NewReporter(recorder, recorder).Fail(ctx, &commonv1.DocumentUploaded{DocumentId: "doc", OwnerId: "owner"}, cause)
			if tc.fail == contracts.StatusFailed {
				if !errors.Is(err, contracts.ErrDurability) || !errors.Is(err, recorder.err) {
					t.Fatalf("durability failure lost: %v", err)
				}
			} else if !errors.Is(err, cause) {
				t.Fatalf("original processing failure lost: %v", err)
			}
			if !slices.Equal(recorder.ops, tc.wantOps) {
				t.Fatalf("operations = %v, want %v", recorder.ops, tc.wantOps)
			}
			if event := recorder.event; event != nil {
				if event.GetDocumentId() != "doc" || event.GetOwnerId() != "owner" || event.GetMessage() != cause.Error() {
					t.Fatalf("event identity or error changed: %v", event)
				}
				if _, err := time.Parse(time.RFC3339, event.GetTimestamp()); err != nil {
					t.Fatalf("invalid event timestamp: %v", err)
				}
			}
		})
	}
}

func TestParsedDeliveryStoresBeforePublishing(t *testing.T) {
	for _, tc := range []struct {
		name, fail string
		size       int
		key        string
		wantOps    []string
	}{
		{name: "inline boundary", size: InlineTextMax, wantOps: []string{"publish"}},
		{name: "large text", size: InlineTextMax + 1, wantOps: []string{"store", "publish"}},
		{name: "existing reference", key: "raw/doc", wantOps: []string{"publish"}},
		{name: "store failure", size: InlineTextMax + 1, fail: "store", wantOps: []string{"store"}},
		{name: "broker failure", size: InlineTextMax + 1, fail: "publish", wantOps: []string{"store", "publish"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &deliveryRecorder{t: t, fail: tc.fail, err: errors.New("dependency unavailable")}
			text := strings.Repeat("a", tc.size)
			parsed := &commonv1.DocumentParsed{DocumentId: "doc", Generation: 7, Text: text, TextObjectKey: tc.key,
				Metadata: map[string]string{"parser_revision": "test-v1"}, TraceId: "trace"}
			err := PublishParsed(contracts.WithDocumentGeneration(t.Context(), 7), recorder, recorder, parsed)
			if (err != nil) != (tc.fail != "") || (err != nil && !errors.Is(err, recorder.err)) {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(recorder.ops, tc.wantOps) {
				t.Fatalf("operations = %v, want %v", recorder.ops, tc.wantOps)
			}
			if tc.size > InlineTextMax && tc.fail != "store" {
				key := contracts.ParsedObjectKey("doc", text)
				if parsed.GetText() != "" || parsed.GetTextObjectKey() != key || recorder.key != key || recorder.stored != text {
					t.Fatal("stored text and published reference disagree")
				}
			} else if parsed.GetText() != text || parsed.GetTextObjectKey() != tc.key {
				t.Fatal("inline text or original reference changed")
			}
			if parsed.GetGeneration() != 7 || parsed.GetTraceId() != "trace" || parsed.GetMetadata()["parser_revision"] != "test-v1" {
				t.Fatal("parsed document provenance changed")
			}
		})
	}
}
