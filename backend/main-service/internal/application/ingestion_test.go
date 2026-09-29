package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/contracts"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func newIngestion(store *fakeObjectStore, docs *fakeDocs, _ *fakePublisher, chunks *fakeChunks) *IngestionService {
	return NewIngestionService(store, docs, chunks)
}

func TestIngestion_Upload_BinaryDispatches(t *testing.T) {
	store, docs, pub := newObjectStore(), newDocs(), &fakePublisher{}
	svc := newIngestion(store, docs, pub, &fakeChunks{})
	cmd := domain.UploadCommand{
		OwnerID: "alice", Filename: "paper.pdf", Size: 4, Body: strings.NewReader("data"), Hash: "h1",
	}
	doc, _, err := svc.Upload(context.Background(), cmd, "application/pdf")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if doc.GetStatus() != contracts.StatusQueued {
		t.Fatalf("status = %q, want queued", doc.GetStatus())
	}
	if len(docs.requests) != 1 || !docs.requests[0].GetEnqueue() || pub.count() != 0 {
		t.Fatal("upload must persist dispatch atomically with its document")
	}
	if len(store.objects) != 1 {
		t.Fatalf("object must be stored, got %d", len(store.objects))
	}
}

func TestIngestion_Upload_InlineTextRoutesToChunker(t *testing.T) {
	store, docs, pub := newObjectStore(), newDocs(), &fakePublisher{}
	svc := newIngestion(store, docs, pub, &fakeChunks{})
	cmd := domain.UploadCommand{
		OwnerID: "alice", Filename: "n.txt", Size: 5, Body: strings.NewReader("hello"),
		Text: "hello", Hash: "",
	}
	if _, _, err := svc.Upload(context.Background(), cmd, "text/plain; charset=utf-8"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if len(docs.requests) != 1 || docs.requests[0].GetInlineText() != "hello" || !docs.requests[0].GetEnqueue() {
		t.Fatal("inline text missing from durable creation")
	}
}

func TestIngestion_Upload_StoreError(t *testing.T) {
	store := newObjectStore()
	store.putErr = errors.New("s3 down")
	svc := newIngestion(store, newDocs(), &fakePublisher{}, &fakeChunks{})
	cmd := domain.UploadCommand{OwnerID: "a", Filename: "f", Size: 1, Body: strings.NewReader("x")}
	if _, _, err := svc.Upload(context.Background(), cmd, "application/pdf"); err == nil {
		t.Fatal("store error must surface")
	}
}

func TestIngestion_FinalizeObject_DeduplicatesByHash(t *testing.T) {
	docs := newDocs()
	docs.byHash["alice|h1"] = &commonv1.Document{Id: "orig", Filename: "orig.pdf"}
	pub := &fakePublisher{}
	svc := newIngestion(newObjectStore(), docs, pub, &fakeChunks{})

	doc, _, err := svc.FinalizeObject(context.Background(), FinalizeRequest{
		OwnerID: "alice", Filename: "dup.pdf", MIMEType: "application/pdf", ObjectKey: "k", Size: 3, ContentHash: "h1",
	})
	if err != nil {
		t.Fatalf("FinalizeObject: %v", err)
	}
	if doc.GetStatus() != contracts.StatusIndexed {
		t.Fatalf("duplicate must be marked indexed, got %q", doc.GetStatus())
	}
	if pub.count() != 0 {
		t.Fatal("duplicate must not be published into the pipeline")
	}
}

// Доступность брокера не влияет на принятую в PostgreSQL загрузку.
func TestIngestion_FinalizeObject_BrokerOutageLeavesQueuedIntent(t *testing.T) {
	docs := newDocs()
	pub := &fakePublisher{err: errors.New("amqp down")}
	svc := newIngestion(newObjectStore(), docs, pub, &fakeChunks{})
	doc, _, err := svc.FinalizeObject(
		t.Context(),
		FinalizeRequest{
			OwnerID:   "alice",
			Filename:  "f.pdf",
			MIMEType:  "application/pdf",
			ObjectKey: "k",
			Size:      1,
		},
	)
	if err != nil || doc.GetStatus() != contracts.StatusQueued || !docs.requests[0].GetEnqueue() {
		t.Fatalf("durable intake failed: %v %v", doc, err)
	}
}

func TestIngestion_FinalizeObject_CreateError(t *testing.T) {
	docs := newDocs()
	docs.createErr = errors.New("db down")
	svc := newIngestion(newObjectStore(), docs, &fakePublisher{}, &fakeChunks{})
	if _, _, err := svc.FinalizeObject(context.Background(), FinalizeRequest{OwnerID: "a", Filename: "f"}); err == nil {
		t.Fatal("create error must surface")
	}
}

func TestIngestion_DocumentReads(t *testing.T) {
	docs := newDocs()
	docs.docs["d1"] = &commonv1.Document{Id: "d1", OwnerId: "alice"}
	docs.docs["d2"] = &commonv1.Document{Id: "d2", OwnerId: "bob"}
	svc := newIngestion(newObjectStore(), docs, &fakePublisher{}, &fakeChunks{})

	if _, err := svc.GetDocument(context.Background(), "bob", "d1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign doc must be forbidden, got %v", err)
	}
	got, err := svc.GetDocument(context.Background(), "alice", "d1")
	if err != nil || got.GetId() != "d1" {
		t.Fatalf("owner doc read: %v / %v", got, err)
	}
	mine, err := svc.ListDocuments(context.Background(), "alice")
	if err != nil || len(mine) != 1 {
		t.Fatalf("ListDocuments: %v / %d", err, len(mine))
	}
	all, err := svc.ListAllDocuments(t.Context(), "admin")
	if err != nil || len(all) != 2 {
		t.Fatalf("ListAllDocuments must span everyone, got %d", len(all))
	}
}

func TestIngestion_DocumentChunks(t *testing.T) {
	docs := newDocs()
	docs.docs["d1"] = &commonv1.Document{Id: "d1", OwnerId: "alice"}
	cr := &fakeChunks{}
	svc := newIngestion(newObjectStore(), docs, &fakePublisher{}, cr)

	if _, _, err := svc.DocumentChunks(context.Background(), "bob", false, "d1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner must be forbidden, got %v", err)
	}
	if _, _, err := svc.DocumentChunks(context.Background(), "bob", true, "d1"); err != nil {
		t.Fatalf("privileged caller must read any doc, got %v", err)
	}
	if _, _, err := svc.DocumentChunks(context.Background(), "alice", false, "d1"); err != nil {
		t.Fatalf("owner must read own doc, got %v", err)
	}
	cr.err = errors.New("chunks down")
	if _, _, err := svc.DocumentChunks(context.Background(), "alice", false, "d1"); err == nil {
		t.Fatal("chunk read error must surface")
	}
}

func TestIngestion_OpenDocument(t *testing.T) {
	store, docs := newObjectStore(), newDocs()
	store.objects["alice/d1/f.pdf"] = []byte("PDFDATA")
	docs.docs["d1"] = &commonv1.Document{Id: "d1", OwnerId: "alice", ObjectKey: "alice/d1/f.pdf"}
	svc := newIngestion(store, docs, &fakePublisher{}, &fakeChunks{})

	if _, _, err := svc.OpenDocument(context.Background(), "bob", false, "d1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner must be forbidden, got %v", err)
	}
	rc, doc, err := svc.OpenDocument(context.Background(), "alice", false, "d1")
	if err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	defer func() { _ = rc.Close() }()
	data, _ := io.ReadAll(rc)
	if string(data) != "PDFDATA" || doc.GetId() != "d1" {
		t.Fatalf("unexpected stream: %q / %v", data, doc)
	}
	store.getErr = errors.New("s3 down")
	if _, _, err := svc.OpenDocument(context.Background(), "alice", true, "d1"); err == nil {
		t.Fatal("store error must surface")
	}
}
