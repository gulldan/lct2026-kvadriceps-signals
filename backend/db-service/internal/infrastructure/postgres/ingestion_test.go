package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/contracts"
	commonv1 "github.com/example/db-service/internal/platform/genproto/common/v1"
	"github.com/example/db-service/internal/platform/indexdoc"
	"google.golang.org/protobuf/proto"
)

func TestIngestionOutboxRecoversAmbiguousPublishAndFencesGenerations(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewDocumentRepo(db)
	doc := &domain.Document{ID: uuid.NewV4().String(), OwnerID: uuid.NewV4().String(), Filename: "f.txt", MIMEType: "text/plain",
		ObjectKey: "tests/" + uuid.NewV4().String(), ContentHash: strings.Repeat("a", 64), Status: "queued", Enqueue: true,
		InlineText: "hello", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM documents WHERE id=$1`, doc.ID)
		if err != nil {
			t.Error(err)
		}
	})
	if err := repo.Create(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	duplicate := *doc
	duplicate.Enqueue = true
	duplicate.InlineText = "hello"
	if err := repo.Create(t.Context(), &duplicate); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var delivered int
	_, err := repo.DispatchOne(ctx, func(_ context.Context, _ string, event proto.Message) error {
		parsed, ok := event.(*commonv1.DocumentParsed)
		if !ok || parsed.GetGeneration() != 1 {
			t.Fatal("generation not persisted")
		}
		delivered++
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("cancelled commit unexpectedly succeeded")
	}
	sent, err := repo.DispatchOne(t.Context(), func(_ context.Context, _ string, _ proto.Message) error { delivered++; return nil })
	if err != nil || !sent || delivered != 2 {
		t.Fatalf("lost pending event: %v %v %d", sent, err, delivered)
	}
	if sent, err = repo.DispatchOne(
		t.Context(),
		func(
			context.Context,
			string,
			proto.Message,
		) error {
			return errors.New(
				"unexpected",
			)
		},
	); err != nil ||
		sent {
		t.Fatalf("duplicate creation produced another event: %v %v", sent, err)
	}
	if err = repo.UpdateStatus(t.Context(), doc.ID, "indexed", "", new(3)); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"queued", "parsed", "failed"} {
		if err = repo.UpdateStatus(t.Context(), doc.ID, status, "late", nil); err != nil {
			t.Fatal(err)
		}
	}
	current, err := repo.Get(t.Context(), doc.ID)
	if err != nil || current.Status != "indexed" {
		t.Fatalf("terminal state regressed: %+v %v", current, err)
	}
	current, err = repo.Requeue(t.Context(), doc.ID, 1, "")
	if err != nil || current.Generation != 2 {
		t.Fatalf("requeue generation: %+v %v", current, err)
	}
	if _, err = repo.Requeue(t.Context(), doc.ID, 1, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("concurrent requeue accepted")
	}
	if err = repo.UpdateStatus(contracts.WithDocumentGeneration(t.Context(), 1), doc.ID, "indexed", "late", new(9)); err != nil {
		t.Fatal(err)
	}
	current, _ = repo.Get(t.Context(), doc.ID)
	if current.Status != "queued" || current.ChunkCount != 3 {
		t.Fatalf("stale writer changed generation: %+v", current)
	}
	indexes := postgres.NewIndexRepo(db)
	if _, err = indexes.Begin(
		t.Context(),
		indexdoc.Lease{
			DocumentID: doc.ID,
			OwnerID:    doc.OwnerID,
			Token:      uuid.NewV4().String(),
			Generation: 1,
		},
	); err == nil {
		t.Fatal("stale event acquired publication lease")
	}
	job, err := indexes.Begin(
		t.Context(),
		indexdoc.Lease{
			DocumentID: doc.ID,
			OwnerID:    doc.OwnerID,
			Token:      uuid.NewV4().String(),
			Generation: 2,
		},
	)
	if err != nil || job.Source.Version != 0 || job.Revision() != doc.ContentHash {
		t.Fatalf("ordinary document cannot use immutable indexing: %+v %v", job, err)
	}
	bad := *doc
	bad.ID = uuid.NewV4().String()
	bad.Enqueue = true
	bad.InlineText = strings.Repeat("x", 6<<20)
	if err = repo.Create(t.Context(), &bad); err == nil {
		t.Fatal("oversized event committed")
	}
	if _, err = repo.Get(t.Context(), bad.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("document survived failed outbox insert")
	}
}
