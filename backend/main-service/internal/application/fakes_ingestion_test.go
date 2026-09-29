package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/example/main-service/internal/platform/contracts"
	"io"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/storage"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	putErr  error
	getErr  error
}

func newObjectStore() *fakeObjectStore { return &fakeObjectStore{objects: map[string][]byte{}} }

func (f *fakeObjectStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	if f.putErr != nil {
		return f.putErr
	}
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
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	data, ok := f.objects[key]
	f.mu.Unlock()
	if !ok {
		return nil, errStoreMiss
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

var errStoreMiss = errors.New("object not found")

type fakeDocs struct {
	requests      []*dbv1.CreateDocumentRequest
	requeued      []string
	mu            sync.Mutex
	docs          map[string]*commonv1.Document
	byHash        map[string]*commonv1.Document
	createErr     error
	getErr        error
	findErr       error
	listErr       error
	statusUpdates int
}

func newDocs() *fakeDocs {
	return &fakeDocs{docs: map[string]*commonv1.Document{}, byHash: map[string]*commonv1.Document{}}
}

func (f *fakeDocs) CreateDocument(
	_ context.Context, req *dbv1.CreateDocumentRequest,
) (*commonv1.Document, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
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
	if req.GetContentHash() != "" {
		f.byHash[req.GetOwnerId()+"|"+req.GetContentHash()] = d
	}
	return d, nil
}

func (f *fakeDocs) GetDocument(_ context.Context, id string) (*commonv1.Document, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return d, nil
}

func (f *fakeDocs) FindDocumentByHash(_ context.Context, ownerID, hash string) (*commonv1.Document, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.byHash[ownerID+"|"+hash]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return d, nil
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
	f.statusUpdates++
	if d, ok := f.docs[id]; ok {
		d.Status = status
	}
	return nil
}

type fakePublisher struct {
	mu     sync.Mutex
	routes []string
	err    error
}

func (f *fakePublisher) PublishProto(_ context.Context, _, routingKey string, _ proto.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.routes = append(f.routes, routingKey)
	return nil
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.routes)
}

type fakeChunks struct {
	chunks map[string][]*llmv1.DocumentChunk
	err    error
}

func (f *fakeChunks) DocumentChunks(
	_ context.Context, _, documentID string,
) ([]*llmv1.DocumentChunk, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.chunks[documentID], nil
}

type fakeMultipart struct {
	mu            sync.Mutex
	createErr     error
	presignErr    error
	completeErr   error
	abortErr      error
	completeCalls int
	abortCalls    int
}

func (f *fakeMultipart) CreateMultipart(_ context.Context, _, _ string) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	return "s3-upload-id", nil
}

func (f *fakeMultipart) PresignUploadPart(
	_ context.Context, _, _ string, _ int32, _ time.Duration,
) (string, error) {
	if f.presignErr != nil {
		return "", f.presignErr
	}
	return "https://s3/part", nil
}

func (f *fakeMultipart) CompleteMultipart(
	_ context.Context, _, _ string, _ []storage.MultipartPart,
) error {
	f.mu.Lock()
	f.completeCalls++
	f.mu.Unlock()
	return f.completeErr
}

func (f *fakeMultipart) AbortMultipart(_ context.Context, _, _ string) error {
	f.mu.Lock()
	f.abortCalls++
	f.mu.Unlock()
	return f.abortErr
}

type fakeSessions struct {
	mu      sync.Mutex
	data    map[string][]byte
	setErr  error
	getErr  error
	delCnt  int
	setJSON func()
}

func newSessions() *fakeSessions { return &fakeSessions{data: map[string][]byte{}} }

func (f *fakeSessions) SetJSON(_ context.Context, key string, v any, _ time.Duration) error {
	if f.setJSON != nil {
		f.setJSON()
	}
	if f.setErr != nil {
		return f.setErr
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.data[key] = b
	f.mu.Unlock()
	return nil
}

func (f *fakeSessions) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	if f.getErr != nil {
		return false, f.getErr
	}
	f.mu.Lock()
	b, ok := f.data[key]
	f.mu.Unlock()
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(b, dest); err != nil {
		return false, err
	}
	return true, nil
}

func (f *fakeSessions) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delCnt += len(keys)
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}

func (f *fakeDocs) RequeueDocument(ctx context.Context, id string, _ int64, inline string) (*commonv1.Document, error) {
	f.requeued = append(f.requeued, inline)
	return f.GetDocument(ctx, id)
}

func (f *fakeDocs) DocumentPage(ctx context.Context, q contracts.DocumentQuery) (*dbv1.DocumentPageResponse, error) {
	owner := q.ReaderID
	if q.AllOwners {
		owner = ""
	}
	docs, err := f.ListDocuments(ctx, owner)
	return &dbv1.DocumentPageResponse{Documents: docs, Total: int64(len(docs))}, err
}
func (f *fakeDocs) DocumentStats(context.Context, contracts.DocumentQuery) (contracts.DocumentStats, error) {
	return contracts.DocumentStats{}, f.listErr
}
