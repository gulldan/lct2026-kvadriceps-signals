package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
)

type statusCall struct {
	status string
	msg    string
	count  int32
	hasCnt bool
}

type capturingStatus struct{ calls []statusCall }

func (c *capturingStatus) UpdateDocumentStatus(
	_ context.Context, _, status, message string, chunkCount *int32,
) error {
	call := statusCall{status: status, msg: message, count: 0, hasCnt: chunkCount != nil}
	if chunkCount != nil {
		call.count = *chunkCount
	}
	c.calls = append(c.calls, call)
	return nil
}

func (c *capturingStatus) last() statusCall { return c.calls[len(c.calls)-1] }

type staticStore struct{ body []byte }

func (s staticStore) GetBytes(context.Context, string) ([]byte, error) { return s.body, nil }

// failingSearch — отказ лексического хранилища после записи векторов, то есть частичная двойная запись.
type failingSearch struct{}

func (failingSearch) Index(context.Context, []SearchDoc) error {
	return errors.New("opensearch down")
}

func TestProcess_TruncationRecordsStructuredFlag(t *testing.T) {
	const textMax = 64 // байты: крошечный, чтобы тело его превысило
	big := []byte(strings.Repeat("материаловедение ", 200))
	status := &capturingStatus{}
	ix := New(
		splitter.NewRecursive(40, 8),
		status, dummyEmbedder{}, &capturingVectors{}, noopSearch{}, noopPub{},
		nil, staticStore{body: big}, // pacer, store — текст по ссылке
		nil, 0, // tokenizer, maxTokens
		textMax, 8, // textMax — обрезка, batchSize
		1<<20, 10, // splitWindow, overlap
		true, // headers
		nil,  // metrics
	)

	evt := &commonv1.DocumentParsed{
		DocumentId:    "doc-trunc",
		OwnerId:       "u1",
		Filename:      "big.txt",
		TextObjectKey: "blob/key",
	}
	if err := ix.Process(contracts.WithAttempt(context.Background(), contracts.MaxAttempts-1), evt); err != nil {
		t.Fatalf("Process: %v", err)
	}

	final := status.last()
	if final.status != contracts.StatusIndexed {
		t.Fatalf("terminal status = %q, want %q", final.status, contracts.StatusIndexed)
	}
	if !strings.Contains(final.msg, "[truncated ") || !strings.Contains(final.msg, "bytes]") {
		t.Fatalf("status_msg missing structured truncation marker: %q", final.msg)
	}
	if !strings.Contains(final.msg, "усеч") {
		t.Fatalf("status_msg missing human truncation note: %q", final.msg)
	}
}

func TestProcess_PartialDualWriteMarksFailed(t *testing.T) {
	status := &capturingStatus{}
	ix := New(
		splitter.NewRecursive(40, 8),
		status, dummyEmbedder{}, &capturingVectors{}, failingSearch{}, noopPub{},
		nil, nil, // pacer, store — текст в сообщении
		nil, 0,
		0, 4,
		1<<20, 8,
		true, // headers
		nil,
	)
	evt := &commonv1.DocumentParsed{
		DocumentId: "doc-partial",
		OwnerId:    "u1",
		Filename:   "x.txt",
		Text: "Достаточно длинный текст чтобы образовать хотя бы один чанк для индексации точно. " +
			"Второе предложение добавляет ещё немного слов для надёжности проверки здесь.",
	}
	err := ix.Process(contracts.WithAttempt(context.Background(), contracts.MaxAttempts-1), evt)
	if err == nil {
		t.Fatal("expected Process to error on lexical-store failure")
	}
	final := status.last()
	if final.status != contracts.StatusFailed {
		t.Fatalf("terminal status = %q, want %q (partial dual-write must not be a clean indexed)",
			final.status, contracts.StatusFailed)
	}
}
