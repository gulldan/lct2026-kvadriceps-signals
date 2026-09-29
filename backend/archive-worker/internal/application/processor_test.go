package application

import (
	"context"
	"errors"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/example/archive-worker/internal/domain"
	"github.com/example/archive-worker/internal/platform/contracts"

	commonv1 "github.com/example/archive-worker/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/archive-worker/internal/platform/genproto/db/v1"
)

type fakeExtractor struct {
	res *domain.Extraction
	err error
}

func (f *fakeExtractor) Extract(_ context.Context, _, _, _ string) (*domain.Extraction, error) {
	return f.res, f.err
}

type fakeCatalog struct {
	mu         sync.Mutex
	created    int
	queued     int
	failFor    map[string]bool
	hashes     map[string]bool
	statusByID map[string]string
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		failFor:    map[string]bool{},
		hashes:     map[string]bool{},
		statusByID: map[string]string{},
	}
}

func (f *fakeCatalog) CreateDocument(_ context.Context, req *dbv1.CreateDocumentRequest) (*commonv1.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFor[req.GetFilename()] {
		return nil, errors.New("db down")
	}
	f.created++
	if req.GetEnqueue() {
		f.queued++
	}
	return &commonv1.Document{
		Id:        "doc-" + req.GetFilename(),
		OwnerId:   req.GetOwnerId(),
		Filename:  req.GetFilename(),
		MimeType:  req.GetMimeType(),
		Size:      req.GetSize(),
		ObjectKey: req.GetObjectKey(),
	}, nil
}

func (f *fakeCatalog) FindDocumentByHash(_ context.Context, _, hash string) (*commonv1.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hashes[hash] {
		return &commonv1.Document{Id: "existing"}, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeCatalog) UpdateDocumentStatus(_ context.Context, id, status, _ string, _ *int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusByID[id] = status
	return nil
}

type fakePublisher struct {
	mu            sync.Mutex
	published     int
	failForFiles  map[string]bool
	publishErrAll bool
}

func (f *fakePublisher) PublishProto(_ context.Context, exchange, _ string, msg proto.Message) error {
	if exchange == contracts.ExchangeEvents {
		return nil
	}
	if f.publishErrAll {
		return errors.New("broker did not confirm")
	}
	if du, ok := msg.(*commonv1.DocumentUploaded); ok && f.failForFiles[du.GetFilename()] {
		return errors.New("broker did not confirm")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published++
	return nil
}

func archiveEvt() *commonv1.DocumentUploaded {
	return &commonv1.DocumentUploaded{
		DocumentId: "archive-1",
		OwnerId:    "owner-1",
		ObjectKey:  "uploads/archive.zip",
		Filename:   "archive.zip",
		MimeType:   "application/zip",
	}
}

func entries(paths ...string) []domain.Entry {
	out := make([]domain.Entry, 0, len(paths))
	for _, p := range paths {
		out = append(out, domain.Entry{Path: p, ObjectKey: "extracted/" + p, MIMEType: "text/plain", Size: 10})
	}
	return out
}

func TestProcess_RetriesUnregisteredEntries(t *testing.T) {
	cat := newFakeCatalog()
	cat.failFor["b.txt"] = true
	pub := &fakePublisher{publishErrAll: true}
	p := New(&fakeExtractor{res: &domain.Extraction{Entries: entries("a.txt", "b.txt", "c.txt")}}, cat, pub, 0)
	if err := p.Process(t.Context(), archiveEvt()); err == nil {
		t.Fatal("failed entry was forgotten")
	}
	if cat.queued != 2 || cat.statusByID["archive-1"] == contracts.StatusIndexed {
		t.Fatal("partial archive reported complete")
	}
	delete(cat.failFor, "b.txt")
	if err := p.Process(t.Context(), archiveEvt()); err != nil {
		t.Fatal(err)
	}
	if cat.statusByID["archive-1"] != contracts.StatusIndexed {
		t.Fatal("recovered archive not completed")
	}
}

// Сбой брокера не отменяет уже принятую транзакционную отправку дочерних документов.
func TestProcess_BrokerOutageDoesNotLoseChildren(t *testing.T) {
	cat := newFakeCatalog()
	pub := &fakePublisher{publishErrAll: true}
	p := New(&fakeExtractor{res: &domain.Extraction{Entries: entries("a.txt", "b.txt")}}, cat, pub, 0)
	if err := p.Process(t.Context(), archiveEvt()); err != nil {
		t.Fatal(err)
	}
	if cat.queued != 2 || pub.published != 0 {
		t.Fatal("children bypassed the durable outbox")
	}
}

func TestProcess_ExtractFailureDeadLetters(t *testing.T) {
	cat := newFakeCatalog()
	p := New(&fakeExtractor{err: errors.New("extract failed")}, cat, &fakePublisher{}, 0)
	if err := p.Process(contracts.WithAttempt(t.Context(), contracts.MaxAttempts-1), archiveEvt()); err == nil {
		t.Fatal("error lost")
	}
	if cat.statusByID["archive-1"] != contracts.StatusFailed {
		t.Fatal("terminal failure not persisted")
	}
}

func TestProcess_SkippedEntriesCountedNotFailed(t *testing.T) {
	cat := newFakeCatalog()
	pub := &fakePublisher{}
	ext := &fakeExtractor{res: &domain.Extraction{Entries: entries("a.txt"), Skipped: 3}}

	p := New(ext, cat, pub, 0)
	if err := p.Process(context.Background(), archiveEvt()); err != nil {
		t.Fatalf("Process returned error, want nil: %v", err)
	}
	if cat.created != 1 {
		t.Errorf("created = %d, want 1", cat.created)
	}
	if got := cat.statusByID["archive-1"]; got != contracts.StatusIndexed {
		t.Errorf("archive status = %q, want %q", got, contracts.StatusIndexed)
	}
}

func TestProcess_TooManyEntriesFails(t *testing.T) {
	cat := newFakeCatalog()
	pub := &fakePublisher{}
	ext := &fakeExtractor{res: &domain.Extraction{Entries: entries("a.txt", "b.txt", "c.txt")}}

	p := New(ext, cat, pub, 1)
	if err := p.Process(context.Background(), archiveEvt()); err == nil {
		t.Fatal("Process returned nil, want error (entry count over limit)")
	}
	if cat.created != 0 {
		t.Errorf("created = %d, want 0", cat.created)
	}
}
