package amqp_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/example/office-parser/internal/application"
	"github.com/example/office-parser/internal/domain"
	"github.com/example/office-parser/internal/interfaces/amqp"

	commonv1 "github.com/example/office-parser/internal/platform/genproto/common/v1"
)

// recordingExtractor отвечает «простой текст», поэтому процессор не трогает хранилище.
type recordingExtractor struct{}

func (recordingExtractor) Extract(context.Context, []byte, string, string) (domain.ExtractionResult, error) {
	return domain.ExtractionResult{}, nil
}
func (recordingExtractor) IsPlainText(string, string) bool { return true }

type nopStore struct{}

func (nopStore) GetBytes(context.Context, string) ([]byte, error) { return nil, nil }
func (nopStore) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}

type nopStatus struct{}

func (nopStatus) UpdateDocumentStatus(context.Context, string, string, string, *int32) error {
	return nil
}

type capturingPublisher struct{ ids []string }

func (c *capturingPublisher) PublishProto(_ context.Context, _, _ string, msg proto.Message) error {
	if p, ok := msg.(*commonv1.DocumentParsed); ok {
		c.ids = append(c.ids, p.GetDocumentId())
	}
	return nil
}

type failingStore struct{}

func (failingStore) GetBytes(context.Context, string) ([]byte, error) {
	return nil, errors.New("download failed")
}
func (failingStore) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func TestHandlerDispatches(t *testing.T) {
	pub := &capturingPublisher{}
	p := application.New(recordingExtractor{}, nopStore{}, nopStatus{}, pub)
	body, err := proto.Marshal(&commonv1.DocumentUploaded{
		DocumentId: "doc-42", OwnerId: "o", ObjectKey: "raw/doc-42",
		Filename: "a.txt", MimeType: "", Size: 0, TraceId: "", ArchiveDepth: 0,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if herr := amqp.Handler(p)(context.Background(), body); herr != nil {
		t.Fatalf("Handler: %v", herr)
	}
	if len(pub.ids) == 0 || pub.ids[0] != "doc-42" {
		t.Fatalf("published ids = %v, want [doc-42]", pub.ids)
	}
}

func TestHandlerBadBody(t *testing.T) {
	p := application.New(recordingExtractor{}, nopStore{}, nopStatus{}, &capturingPublisher{})
	err := amqp.Handler(p)(context.Background(), []byte{0xff, 0xff, 0xff})
	if err == nil {
		t.Fatalf("Handler expected unmarshal error, got nil")
	}
}

func TestHandlerPropagatesProcessError(t *testing.T) {
	// Двоичный путь, чтобы скачивание и его сбой действительно произошли.
	p := application.New(downloadingExtractor{}, failingStore{}, nopStatus{}, &capturingPublisher{})
	body, err := proto.Marshal(&commonv1.DocumentUploaded{
		DocumentId: "doc-err", OwnerId: "o", ObjectKey: "raw/doc-err",
		Filename: "a.docx", MimeType: "", Size: 0, TraceId: "", ArchiveDepth: 0,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if herr := amqp.Handler(p)(context.Background(), body); herr == nil {
		t.Fatalf("Handler expected processing error, got nil")
	}
}

type downloadingExtractor struct{}

func (downloadingExtractor) Extract(context.Context, []byte, string, string) (domain.ExtractionResult, error) {
	return domain.ExtractionResult{}, nil
}
func (downloadingExtractor) IsPlainText(string, string) bool { return false }
