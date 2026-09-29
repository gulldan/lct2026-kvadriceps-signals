package application

import (
	"context"
	"errors"
	"github.com/example/main-service/internal/platform/contracts"
	"testing"

	"github.com/example/main-service/internal/platform/storage"
)

func newUploads(mp *fakeMultipart, store *fakeObjectStore, sess *fakeSessions, ing *IngestionService) *UploadSessionService {
	return NewUploadSessionService(mp, store, sess, ing, 0)
}

func ingestionForUploads() (*IngestionService, *fakePublisher) {
	pub := &fakePublisher{}
	return newIngestion(newObjectStore(), newDocs(), pub, &fakeChunks{}), pub
}

func TestUploads_Begin_Success(t *testing.T) {
	mp, sess := &fakeMultipart{}, newSessions()
	ing, _ := ingestionForUploads()
	svc := newUploads(mp, newObjectStore(), sess, ing)

	s, err := svc.Begin(context.Background(), "alice", "big.bin", "application/octet-stream", 40<<20, false)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if s.PartCount < 1 || s.PartSize < minPartSize {
		t.Fatalf("bad layout: %+v", s)
	}
	if len(sess.data) != 1 {
		t.Fatalf("session must be persisted, got %d", len(sess.data))
	}
}

// Предельный размер проверяется до открытия S3-загрузки.
func TestUploads_Begin_RejectsResourceAmplification(t *testing.T) {
	svc := newUploads(&fakeMultipart{}, newObjectStore(), newSessions(), mustIngestion())
	for _, file := range []struct {
		name, mime string
		size       int64
	}{
		{"huge.bin", "application/octet-stream", 65 << 20}, {"huge.zip", "application/zip", (1 << 30) + 1},
	} {
		if _, err := svc.Begin(t.Context(), "alice", file.name, file.mime, file.size, false); err == nil {
			t.Fatal("oversized upload accepted")
		}
	}
}

func TestUploads_Begin_BadSize(t *testing.T) {
	svc := newUploads(&fakeMultipart{}, newObjectStore(), newSessions(), mustIngestion())
	if _, err := svc.Begin(context.Background(), "a", "f", "", 0, false); err == nil {
		t.Fatal("zero size must error")
	}
	if _, err := svc.Begin(context.Background(), "a", "f", "", (200<<30)+1, false); err == nil {
		t.Fatal("over-limit size must error")
	}
}

func TestUploads_Begin_PersistFailureAborts(t *testing.T) {
	mp := &fakeMultipart{}
	sess := newSessions()
	sess.setErr = errors.New("valkey down")
	svc := newUploads(mp, newObjectStore(), sess, mustIngestion())
	if _, err := svc.Begin(context.Background(), "a", "f", "", 1<<20, false); err == nil {
		t.Fatal("persist failure must surface")
	}
	if mp.abortCalls != 1 {
		t.Fatalf("persist failure must abort the S3 upload, got %d aborts", mp.abortCalls)
	}
}

func TestUploads_Begin_CreateError(t *testing.T) {
	mp := &fakeMultipart{createErr: errors.New("s3 down")}
	svc := newUploads(mp, newObjectStore(), newSessions(), mustIngestion())
	if _, err := svc.Begin(context.Background(), "a", "f", "", 1<<20, false); err == nil {
		t.Fatal("create multipart error must surface")
	}
}

func TestUploads_PartURLs(t *testing.T) {
	mp, sess := &fakeMultipart{}, newSessions()
	svc := newUploads(mp, newObjectStore(), sess, mustIngestion())
	s, err := svc.Begin(context.Background(), "alice", "f.bin", "", 40<<20, false)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	urls, err := svc.PartURLs(context.Background(), "alice", s.ID, 1, 2)
	if err != nil {
		t.Fatalf("PartURLs: %v", err)
	}
	if len(urls) == 0 || urls[0].PartNumber != 1 {
		t.Fatalf("unexpected urls: %+v", urls)
	}
	if _, err := svc.PartURLs(context.Background(), "alice", s.ID, s.PartCount+5, 1); err == nil {
		t.Fatal("out-of-range part must error")
	}
	if _, err := svc.PartURLs(context.Background(), "alice", "nope", 1, 1); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("unknown session must be not-found, got %v", err)
	}
	mp.presignErr = errors.New("presign down")
	if _, err := svc.PartURLs(context.Background(), "alice", s.ID, 1, 1); err == nil {
		t.Fatal("presign failure must surface")
	}
}

func TestUploads_Complete_Success(t *testing.T) {
	mp, sess := &fakeMultipart{}, newSessions()
	ing, pub := ingestionForUploads()
	store := newObjectStore()
	svc := newUploads(mp, store, sess, ing)
	s, err := svc.Begin(context.Background(), "alice", "f.bin", "application/octet-stream", 20<<20, false)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	store.objects[s.ObjectKey] = make([]byte, s.Size)
	parts := make([]storage.MultipartPart, s.PartCount)
	for i := range parts {
		parts[i] = storage.MultipartPart{PartNumber: int32(i + 1), ETag: "etag"}
	}
	doc, _, err := svc.Complete(context.Background(), "alice", s.ID, parts)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if doc == nil {
		t.Fatal("Complete must return a document")
	}
	if pub.count() != 0 || doc.GetStatus() != contracts.StatusQueued {
		t.Fatal("Complete must persist dispatch with the document")
	}
	repeated, _, repeatErr := svc.Complete(t.Context(), "alice", s.ID, parts)
	if repeatErr != nil || repeated.GetId() != doc.GetId() {
		t.Fatal("completion retry created a new logical upload")
	}
}

func TestUploads_Complete_Errors(t *testing.T) {
	mp, sess := &fakeMultipart{}, newSessions()
	svc := newUploads(mp, newObjectStore(), sess, mustIngestion())
	s, _ := svc.Begin(context.Background(), "alice", "f.bin", "", 20<<20, false)
	if _, _, err := svc.Complete(context.Background(), "alice", s.ID, nil); err == nil {
		t.Fatal("part-count mismatch must error")
	}
	parts := make([]storage.MultipartPart, s.PartCount)
	mp.completeErr = errors.New("complete down")
	if _, _, err := svc.Complete(context.Background(), "alice", s.ID, parts); err == nil {
		t.Fatal("completion failure must surface")
	}
}

func TestUploads_Abort(t *testing.T) {
	mp, sess := &fakeMultipart{}, newSessions()
	svc := newUploads(mp, newObjectStore(), sess, mustIngestion())
	s, _ := svc.Begin(context.Background(), "alice", "f.bin", "", 20<<20, false)
	if err := svc.Abort(context.Background(), "alice", s.ID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if mp.abortCalls != 1 {
		t.Fatalf("abort must hit S3, got %d", mp.abortCalls)
	}
	if err := svc.Abort(context.Background(), "alice", "gone"); err != nil {
		t.Fatalf("missing session must be a no-op, got %v", err)
	}
}

func TestUploads_Complete_HashesForDedup(t *testing.T) {
	mp, sess := &fakeMultipart{}, newSessions()
	store := newObjectStore()
	docs, pub := newDocs(), &fakePublisher{}
	ing := newIngestion(store, docs, pub, &fakeChunks{})
	svc := NewUploadSessionService(mp, store, sess, ing, 0)
	s, err := svc.Begin(context.Background(), "alice", "f.bin", "application/octet-stream", 7, false)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	store.objects[s.ObjectKey] = []byte("payload")
	parts := make([]storage.MultipartPart, s.PartCount)
	if _, _, err := svc.Complete(context.Background(), "alice", s.ID, parts); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(docs.byHash) == 0 {
		t.Fatal("Complete within hash limit must record a content hash for dedup")
	}
}

func mustIngestion() *IngestionService {
	return newIngestion(newObjectStore(), newDocs(), &fakePublisher{}, &fakeChunks{})
}
