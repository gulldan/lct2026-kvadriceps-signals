package postgres_test

import (
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func publicationCluster(owner string) *domain.Cluster {
	return &domain.Cluster{ID: uuid.NewV4().String(), OwnerID: owner, Label: "Тема", Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

func TestClusterPublicationAtomicIdempotentAndFenced(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewClusterRepo(db)
	owner, first := uuid.NewV4().String(), uuid.NewV4().String()
	initial := publicationCluster(owner)
	if _, err := repo.Publish(t.Context(), owner, first, "", "hash-1", []*domain.Cluster{initial}); err != nil {
		t.Fatal(err)
	}
	broken := publicationCluster(owner)
	broken.Params = []byte("invalid JSON")
	_, err := repo.Publish(
		t.Context(),
		owner,
		uuid.NewV4().String(),
		first,
		"broken",
		[]*domain.Cluster{
			publicationCluster(
				owner,
			),
			broken,
		},
	)
	if err == nil {
		t.Fatal("invalid batch accepted")
	}
	listed, err := repo.ListByOwner(t.Context(), owner)
	if err != nil || len(listed) != 1 || listed[0].ID != initial.ID {
		t.Fatalf("failed publication changed active board: %+v %v", listed, err)
	}
	var stored int
	if stepErr := db.Pool.QueryRow(
		t.Context(),
		`SELECT count(*) FROM clusters WHERE owner_id=$1`,
		owner,
	).Scan(
		&stored,
	); stepErr != nil ||
		stored != 1 {
		t.Fatalf("partial rows persisted: %d %v", stored, stepErr)
	}
	var wg sync.WaitGroup
	ids := []string{uuid.NewV4().String(), uuid.NewV4().String()}
	errs := make([]error, 2)
	for i := range ids {
		wg.Go(func() {
			_, errs[i] = repo.Publish(t.Context(), owner, ids[i], first, ids[i], []*domain.Cluster{publicationCluster(owner)})
		})
	}
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent publications were not fenced: %v", errs)
	}
	head, err := repo.Head(t.Context(), owner)
	if err != nil || head == first {
		t.Fatalf("head did not advance: %q %v", head, err)
	}
	replayed, err := repo.Publish(t.Context(), owner, first, "", "hash-1", nil)
	if err != nil || len(replayed) != 1 || replayed[0].ID != initial.ID {
		t.Fatalf("ambiguous response retry changed identities: %+v %v", replayed, err)
	}
	after, err := repo.Head(t.Context(), owner)
	if err != nil || after != head {
		t.Fatalf("old retry replaced newer board: %q %v", after, err)
	}
	if _, err = repo.Publish(t.Context(), owner, first, "", "changed-hash", nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("request key reused with different content: %v", err)
	}
	listed, err = repo.ListByOwner(t.Context(), owner)
	if err != nil || len(listed) != 1 || listed[0].ID == initial.ID {
		t.Fatalf("historical clusters leaked into current board: %+v %v", listed, err)
	}
	if _, err := repo.Get(t.Context(), initial.ID); err != nil {
		t.Fatalf("historical foreign-key target removed: %v", err)
	}
}
