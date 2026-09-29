package application

import (
	"context"
	"fmt"

	"github.com/example/chunk-splitter/internal/platform/indexdoc"
)

func writeSourceProjections(ctx context.Context, vectors ConfirmVectors, search ConfirmSearch,
	capture *captureIndex, m indexdoc.Manifest) error {
	const batch = 128
	for start := 0; start < len(capture.points); start += batch {
		end := min(start+batch, len(capture.points))
		points, docs := capture.points[start:end], capture.docs[start:end]
		if err := vectors.Upsert(ctx, points); err != nil {
			return fmt.Errorf("write source vectors: %w", err)
		}
		if err := search.Index(ctx, docs); err != nil {
			return fmt.Errorf("write source lexical index: %w", err)
		}
		ids := make([]string, len(points))
		for i, point := range points {
			ids[i] = point.ID
		}
		if err := vectors.Confirm(ctx, m.OwnerID, m.DocumentID, m.SnapshotID, ids, m.Model.Dimension); err != nil {
			return err
		}
		if err := search.Confirm(ctx, m.OwnerID, m.DocumentID, m.SnapshotID, ids); err != nil {
			return err
		}
	}
	return search.Refresh(ctx)
}

func prunePreviousIndexes(ctx context.Context, vectors ConfirmVectors, search ConfirmSearch,
	job indexdoc.Job) ([]indexdoc.Previous, error) {
	var pruned []indexdoc.Previous
	for _, previous := range job.Previous {
		namespace := job.Lease.DocumentID
		if previous.SnapshotID != "" {
			namespace += ":" + previous.SnapshotID
		}
		for start := 0; start < previous.ChunkCount; start += 128 {
			ids := make([]string, 0, 128)
			for i := start; i < min(start+128, previous.ChunkCount); i++ {
				ids = append(ids, chunkPointID(namespace, i))
			}
			if err := vectors.DeleteIDs(ctx, job.Lease.OwnerID, job.Lease.DocumentID, ids); err != nil {
				return pruned, err
			}
			if err := search.DeleteIDs(ctx, job.Lease.OwnerID, job.Lease.DocumentID, ids); err != nil {
				return pruned, err
			}
		}
		pruned = append(pruned, previous)
	}
	return pruned, nil
}
