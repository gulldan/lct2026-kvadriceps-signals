package application

import (
	"errors"
	"strconv"
	"testing"
	"uuid"

	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func TestPublishClustersCapacity(t *testing.T) {
	for _, count := range []int{2068, 5000, 5001} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			cat := newCatalog()
			svc := newHypService(cat, nil)
			req := &dbv1.PublishClustersRequest{
				RequestId: uuid.NewV4().String(),
				Clusters:  make([]*dbv1.CreateClusterRequest, count),
			}
			for i := range req.Clusters {
				req.Clusters[i] = &dbv1.CreateClusterRequest{Label: "Тема"}
			}
			got, err := svc.PublishClusters(t.Context(), "owner", req)
			if count > 5000 {
				if !errors.Is(err, ErrInvalidArgument) || len(cat.clusters) != 0 {
					t.Fatalf("oversized board was not rejected before publication: %v", err)
				}
				return
			}
			if err != nil || len(got) != count || req.GetOwnerId() != "owner" {
				t.Fatalf("PublishClusters(%d): count=%d owner=%q err=%v", count, len(got), req.GetOwnerId(), err)
			}
		})
	}
}
