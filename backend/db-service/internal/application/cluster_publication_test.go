package application_test

import (
	"errors"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/domain"
)

func TestPublishLargeClusterBoard(t *testing.T) {
	for _, count := range []int{2068, 5000, 5001} {
		clusters := make([]*domain.Cluster, count)
		for i := range clusters {
			clusters[i] = &domain.Cluster{Label: "Тема", DocumentCount: 6, ChunkCount: 12}
		}
		got, err := newRepos().svc().PublishClusters(t.Context(), "owner", uuid.NewV4().String(), "", clusters)
		if count > 5000 {
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("oversized board: %v", err)
			}
			continue
		}
		if err != nil || len(got) != count || got[0].OwnerID != "owner" || got[0].ID == "" {
			t.Fatalf("board of %d clusters: got %d, err=%v", count, len(got), err)
		}
	}
}
