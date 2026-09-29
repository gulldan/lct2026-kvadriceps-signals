package httpapi_test

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/storage"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

type fakeChunks struct {
	chunks map[string][]*llmv1.DocumentChunk
	err    error
}

func (f *fakeChunks) DocumentChunks(_ context.Context, _, documentID string) ([]*llmv1.DocumentChunk, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.chunks[documentID], nil
}

type fakePublisher struct {
	mu     sync.Mutex
	routes []string
}

func (f *fakePublisher) PublishProto(_ context.Context, _, routingKey string, _ proto.Message) error {
	f.mu.Lock()
	f.routes = append(f.routes, routingKey)
	f.mu.Unlock()
	return nil
}

type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newObjectStore() *fakeObjectStore { return &fakeObjectStore{objects: map[string][]byte{}} }

func (f *fakeObjectStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.objects[key] = data
	f.mu.Unlock()
	return nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	data, ok := f.objects[key]
	f.mu.Unlock()
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

type fakeMultipart struct{}

func (f *fakeMultipart) CreateMultipart(_ context.Context, _, _ string) (string, error) {
	return "s3-id", nil
}

func (f *fakeMultipart) PresignUploadPart(
	_ context.Context, _, _ string, _ int32, _ time.Duration,
) (string, error) {
	return "https://s3/part", nil
}

func (f *fakeMultipart) CompleteMultipart(_ context.Context, _, _ string, _ []storage.MultipartPart) error {
	return nil
}

func (f *fakeMultipart) AbortMultipart(_ context.Context, _, _ string) error { return nil }

type fakeSessions struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newSessions() *fakeSessions { return &fakeSessions{data: map[string][]byte{}} }

func (f *fakeSessions) SetJSON(_ context.Context, key string, v any, _ time.Duration) error {
	b, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.data[key] = b
	f.mu.Unlock()
	return nil
}

func (f *fakeSessions) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	f.mu.Lock()
	b, ok := f.data[key]
	f.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, jsonUnmarshal(b, dest)
}

func (f *fakeSessions) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}

type fakeDocs struct {
	requests []*dbv1.CreateDocumentRequest
	requeued []string
	mu       sync.Mutex
	docs     map[string]*commonv1.Document
	listErr  error
	pageHook func(contracts.DocumentQuery) error
}

func newDocs() *fakeDocs { return &fakeDocs{docs: map[string]*commonv1.Document{}} }

func (f *fakeDocs) CreateDocument(_ context.Context, req *dbv1.CreateDocumentRequest) (*commonv1.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := &commonv1.Document{
		Id: "doc-" + req.GetFilename(), OwnerId: req.GetOwnerId(), Filename: req.GetFilename(),
		MimeType: req.GetMimeType(), Size: req.GetSize(), ObjectKey: req.GetObjectKey(),
		SourceMetadata: req.GetSourceMetadata(), Generation: 1,
	}
	if req.GetEnqueue() {
		d.Status = "queued"
	}
	f.requests = append(f.requests, req)
	f.docs[d.GetId()] = d
	return d, nil
}

func (f *fakeDocs) GetDocument(_ context.Context, id string) (*commonv1.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return d, nil
}

func (f *fakeDocs) FindDocumentByHash(_ context.Context, _, _ string) (*commonv1.Document, error) {
	return nil, dbclient.ErrNotFound
}

func (f *fakeDocs) ListDocuments(_ context.Context, ownerID string, _ ...contracts.DocumentScan) ([]*commonv1.Document, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*commonv1.Document, 0, len(f.docs))
	for _, d := range f.docs {
		if ownerID == "" || d.GetOwnerId() == ownerID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDocs) UpdateDocumentStatus(_ context.Context, id, status, _ string, _ *int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.docs[id]; ok {
		d.Status = status
	}
	return nil
}

func (f *fakeDocs) RequeueDocument(ctx context.Context, id string, _ int64, inline string) (*commonv1.Document, error) {
	f.requeued = append(f.requeued, inline)
	return f.GetDocument(ctx, id)
}
