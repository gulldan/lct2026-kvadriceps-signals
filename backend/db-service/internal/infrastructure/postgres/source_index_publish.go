package postgres

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
)

func validateActivation(in indexdoc.Activation) error {
	if err := in.Lease.Validate(); err != nil {
		return err
	}
	m := in.Manifest
	if m.DocumentID != in.Lease.DocumentID || m.OwnerID != in.Lease.OwnerID || m.SnapshotID != in.Lease.Token {
		return indexdoc.ErrInvalid
	}
	return m.Validate()
}

// Prepare записывает полный намеченный набор чанков до любой записи в индексы.
func (r *IndexRepo) Prepare(ctx context.Context, in indexdoc.Activation) (indexdoc.Empty, error) {
	if err := validateActivation(in); err != nil {
		return indexdoc.Empty{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("begin snapshot preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockIndex(ctx, tx, in.Lease); err != nil {
		return indexdoc.Empty{}, err
	}
	var sourceRaw []byte
	var objectKey, contentHash string
	err = tx.QueryRow(ctx, `SELECT source_metadata,object_key,content_hash FROM documents WHERE id=$1 AND owner_id=$2`,
		in.Lease.DocumentID, in.Lease.OwnerID).Scan(&sourceRaw, &objectKey, &contentHash)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("read prepared provenance: %w", err)
	}
	revision, rawHash := contentHash, contentHash
	if len(sourceRaw) > 0 {
		src, decodeErr := sourcedoc.Decode(sourceRaw)
		if decodeErr != nil {
			return indexdoc.Empty{}, decodeErr
		}
		revision, rawHash = src.Revision, src.ContentHash
	}
	if revision != in.Manifest.SourceRevision || rawHash != in.Manifest.RawHash || objectKey != in.Manifest.RawKey {
		return indexdoc.Empty{}, indexdoc.ErrInvalid
	}
	raw, err := json.Marshal(in.Manifest)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("encode prepared snapshot: %w", err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO source_index_snapshots(snapshot_id,document_id,manifest) VALUES($1,$2,$3)
ON CONFLICT(snapshot_id) DO UPDATE SET manifest=source_index_snapshots.manifest
WHERE source_index_snapshots.manifest=EXCLUDED.manifest`, in.Lease.Token, in.Lease.DocumentID, raw)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("save prepared snapshot: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return indexdoc.Empty{}, indexdoc.ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return indexdoc.Empty{}, fmt.Errorf("commit prepared snapshot: %w", err)
	}
	return indexdoc.Empty{}, nil
}

// Activate публикует манифест и статус indexed, только когда оба хранилища сошлись.
func (r *IndexRepo) Activate(ctx context.Context, in indexdoc.Activation) (indexdoc.Empty, error) {
	if err := validateActivation(in); err != nil {
		return indexdoc.Empty{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("begin index activation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockIndex(ctx, tx, in.Lease); err != nil {
		return indexdoc.Empty{}, err
	}
	raw, err := json.Marshal(in.Manifest)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("encode index activation: %w", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE source_index_snapshots SET ready=true,ready_at=coalesce(ready_at,clock_timestamp())
WHERE snapshot_id=$1 AND document_id=$2 AND manifest=$3`, in.Lease.Token, in.Lease.DocumentID, raw)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("activate prepared snapshot: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return indexdoc.Empty{}, indexdoc.ErrConflict
	}
	tag, err = tx.Exec(ctx, `UPDATE source_index_heads SET active_snapshot_id=$2,last_error='',updated_at=clock_timestamp()
WHERE document_id=$1 AND active_snapshot_id IS DISTINCT FROM $2`, in.Lease.DocumentID, in.Lease.Token)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("publish index pointer: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE documents SET status='indexed',status_msg='',chunk_count=$2 WHERE id=$1`,
		in.Lease.DocumentID, in.Manifest.ChunkCount); err != nil {
		return indexdoc.Empty{}, fmt.Errorf("publish index status: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_corpus_epoch SET value=value+$1 WHERE id=true`, tag.RowsAffected()); err != nil {
		return indexdoc.Empty{}, fmt.Errorf("advance source epoch: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return indexdoc.Empty{}, fmt.Errorf("commit source index activation: %w", err)
	}
	return indexdoc.Empty{}, nil
}

// Finish не трогает активный снимок: записывает неудачную попытку и освобождает аренду.
func (r *IndexRepo) Finish(ctx context.Context, in indexdoc.Finish) (indexdoc.Empty, error) {
	if err := in.Lease.Validate(); err != nil {
		return indexdoc.Empty{}, err
	}
	if len(in.Error) > 1000 || len(in.Pruned) > 33 {
		return indexdoc.Empty{}, indexdoc.ErrInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("begin source index finish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	finishJSON, err := json.Marshal(in)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("encode index completion: %w", err)
	}
	var completed bool
	err = tx.QueryRow(ctx, `SELECT coalesce(h.last_finish=$3,false) FROM source_index_heads h
JOIN documents d ON d.id=h.document_id WHERE d.id=$1 AND d.owner_id=$2 FOR UPDATE OF d,h`,
		in.Lease.DocumentID, in.Lease.OwnerID, finishJSON).Scan(&completed)
	if err != nil {
		return indexdoc.Empty{}, indexdoc.ErrNotFound
	}
	if completed {
		return indexdoc.Empty{}, nil
	}
	if err = lockIndex(ctx, tx, in.Lease); err != nil {
		return indexdoc.Empty{}, err
	}
	for _, prior := range in.Pruned {
		if prior.SnapshotID == "" {
			_, err = tx.Exec(ctx, `UPDATE source_index_heads SET legacy_pruned=true WHERE document_id=$1`, in.Lease.DocumentID)
		} else if prior.SnapshotID != in.Lease.Token {
			_, err = tx.Exec(ctx, `UPDATE source_index_snapshots SET pruned=true WHERE document_id=$1 AND snapshot_id=$2
AND snapshot_id<>(SELECT active_snapshot_id FROM source_index_heads WHERE document_id=$1)`, in.Lease.DocumentID, prior.SnapshotID)
		}
		if err != nil {
			return indexdoc.Empty{}, fmt.Errorf("record pruned index: %w", err)
		}
	}
	if in.Error != "" {
		_, err = tx.Exec(ctx, `UPDATE documents SET status='failed',status_msg=$2 WHERE id=$1
AND NOT EXISTS(SELECT 1 FROM source_index_heads WHERE document_id=$1 AND active_snapshot_id IS NOT NULL)`,
			in.Lease.DocumentID, in.Error)
		if err != nil {
			return indexdoc.Empty{}, fmt.Errorf("record source index failure: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE source_index_heads SET lease_token='',leased_until='-infinity',last_error=$3,last_finish=$4,
updated_at=clock_timestamp() WHERE document_id=$1 AND lease_token=$2`, in.Lease.DocumentID, in.Lease.Token, in.Error, finishJSON)
	if err != nil {
		return indexdoc.Empty{}, fmt.Errorf("release source index: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return indexdoc.Empty{}, fmt.Errorf("commit source index finish: %w", err)
	}
	return indexdoc.Empty{}, nil
}
