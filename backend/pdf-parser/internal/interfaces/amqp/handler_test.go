package amqp_test

import (
	"context"
	"io"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/example/pdf-parser/internal/application"
	amqpiface "github.com/example/pdf-parser/internal/interfaces/amqp"
	"github.com/example/pdf-parser/internal/platform/contracts"

	commonv1 "github.com/example/pdf-parser/internal/platform/genproto/common/v1"
)

type stubExtractor struct{ text string }

func (e stubExtractor) Extract(_ context.Context, _ []byte) (string, error) { return e.text, nil }

type stubStore struct{}

func (stubStore) GetBytes(_ context.Context, _ string) ([]byte, error) { return []byte("%PDF"), nil }

func (stubStore) Put(_ context.Context, _ string, _ io.Reader, _ int64, _ string) error { return nil }

type stubStatus struct{}

func (stubStatus) UpdateDocumentStatus(_ context.Context, _, _, _ string, _ *int32) error {
	return nil
}

type recordingPublisher struct{ chunk *commonv1.DocumentParsed }

func (p *recordingPublisher) PublishProto(_ context.Context, exchange, routingKey string, msg proto.Message) error {
	if exchange == contracts.ExchangeEvents {
		return nil
	}
	if routingKey == contracts.RouteChunk {
		if dp, ok := msg.(*commonv1.DocumentParsed); ok {
			p.chunk = dp
		}
	}
	return nil
}

func TestHandler_DecodesAndProcesses(t *testing.T) {
	pub := &recordingPublisher{chunk: nil}
	proc := application.New(stubExtractor{text: "extracted text"}, stubStore{}, stubStatus{}, pub)

	body, err := proto.Marshal(&commonv1.DocumentUploaded{
		DocumentId: "doc-9",
		OwnerId:    "owner-9",
		ObjectKey:  "uploads/x.pdf",
		Filename:   "x.pdf",
		MimeType:   "application/pdf",
	})
	if err != nil {
		t.Fatalf("marshal upload event: %v", err)
	}

	if herr := amqpiface.Handler(proc)(context.Background(), body); herr != nil {
		t.Fatalf("Handler returned error, want nil: %v", herr)
	}
	if pub.chunk == nil {
		t.Fatal("handler did not drive the processor to publish a chunk message")
	}
	if pub.chunk.GetDocumentId() != "doc-9" {
		t.Errorf("decoded document id = %q, want doc-9", pub.chunk.GetDocumentId())
	}
}

func TestHandler_InvalidProtoBody(t *testing.T) {
	pub := &recordingPublisher{chunk: nil}
	proc := application.New(stubExtractor{text: "t"}, stubStore{}, stubStatus{}, pub)

	if err := amqpiface.Handler(proc)(context.Background(), []byte{0x08}); err == nil {
		t.Fatal("Handler returned nil for malformed body, want decode error")
	}
}
