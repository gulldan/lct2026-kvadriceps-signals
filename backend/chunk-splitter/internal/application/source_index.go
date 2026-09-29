package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	"io"
	"time"
	"uuid"

	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/logger"
	"lukechampine.com/blake3"
)

type IndexControl interface {
	GetDocument(context.Context, string) (*commonv1.Document, error)
	BeginIndex(context.Context, indexdoc.Lease) (indexdoc.Job, error)
	RenewIndex(context.Context, indexdoc.Lease) error
	PrepareIndex(context.Context, indexdoc.Activation) error
	ActivateIndex(context.Context, indexdoc.Activation) error
	FinishIndex(context.Context, indexdoc.Finish) error
}

type SnapshotStore interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, io.Reader, int64, string) error
}

type ConfirmVectors interface {
	VectorStore
	Confirm(context.Context, string, string, string, []string, int) error
	DeleteIDs(context.Context, string, string, []string) error
}

type ConfirmSearch interface {
	SearchStore
	Confirm(context.Context, string, string, string, []string) error
	Refresh(context.Context) error
	DeleteIDs(context.Context, string, string, []string) error
}

type sourceIndexing struct {
	control IndexControl
	store   SnapshotStore
	model   indexdoc.Model
}

// EnableSourceIndexes индексирует документы с явным научным провенансом только полными неизменяемыми
// снапшотами; остальные идут старым путём.
func (ix *Indexer) EnableSourceIndexes(control IndexControl, store SnapshotStore, model indexdoc.Model) {
	ix.sourceIndexes = &sourceIndexing{control: control, store: store, model: model}
}

func (ix *Indexer) Process(ctx context.Context, evt *commonv1.DocumentParsed) error {
	if ix.sourceIndexes == nil {
		return ix.process(ctx, evt)
	}
	doc, err := ix.sourceIndexes.control.GetDocument(ctx, evt.GetDocumentId())
	if err != nil {
		return fmt.Errorf("load source index document: %w", err)
	}
	if doc.GetOwnerId() != evt.GetOwnerId() || doc.GetObjectKey() != evt.GetObjectKey() {
		return errors.New("parsed document identity differs from stored source")
	}
	if max(1, doc.GetGeneration()) != max(1, evt.GetGeneration()) {
		return nil
	}
	return ix.processSource(ctx, evt, doc)
}

func (ix *Indexer) processSource(parent context.Context, evt *commonv1.DocumentParsed, doc *commonv1.Document) (result error) {
	deps := ix.sourceIndexes
	vectors, vok := ix.vectors.(ConfirmVectors)
	search, sok := ix.search.(ConfirmSearch)
	if !vok || !sok || deps.model.Name == "" || deps.model.Revision == "" {
		return errors.New("source index requires verifiable projections and a pinned embedding model")
	}
	lease := indexdoc.Lease{
		DocumentID: evt.GetDocumentId(),
		OwnerID:    evt.GetOwnerId(),
		Token:      uuid.NewV4().String(),
		Generation: max(
			1,
			evt.GetGeneration(),
		),
	}
	job, err := deps.control.BeginIndex(parent, lease)
	if err != nil {
		return fmt.Errorf("begin source index: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	heartbeatDone := make(chan struct{})
	go renewSourceLease(ctx, cancel, deps.control, lease, heartbeatDone)
	pruned := []indexdoc.Previous{}
	defer func() {
		cancel()
		<-heartbeatDone
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
		defer stop()
		note := ""
		if result != nil {
			note = "source indexing failed; previous ready snapshot retained"
		}
		finishErr := deps.control.FinishIndex(cleanup, indexdoc.Finish{Lease: lease, Error: note, Pruned: pruned})
		if finishErr != nil {
			result = errors.Join(result, contracts.ErrDurability, finishErr)
		}
	}()
	if err = verifySourceRaw(ctx, deps.store, job); err != nil {
		return err
	}
	capture := &captureIndex{title: doc.GetTitle(), pinnedTitle: doc.GetSourceMetadata() != ""}
	clone := *ix
	clone.status, clone.vectors, clone.search, clone.pub, clone.capture = capture, capture, capture, capture, capture
	clone.textMax = indexdoc.MaxTextBytes
	if err = clone.process(ctx, evt); err != nil {
		return err
	}
	snapshot, err := capture.snapshot(evt, job, deps.model)
	if err != nil {
		return err
	}
	snapshot.Chunking = clone.sourceChunking()
	manifest, err := saveSourceSnapshot(ctx, deps.store, capture.text, snapshot, job)
	if err != nil {
		return err
	}
	activation := indexdoc.Activation{Lease: lease, Manifest: manifest}
	if err = deps.control.PrepareIndex(ctx, activation); err != nil {
		return err
	}
	if err = writeSourceProjections(ctx, vectors, search, capture, manifest); err != nil {
		return err
	}
	if err = deps.control.ActivateIndex(ctx, activation); err != nil {
		return err
	}
	logger.From(ctx).Info().Str("document_id", lease.DocumentID).Str("snapshot_id", lease.Token).
		Int("chunks", manifest.ChunkCount).Msg("source index ready")
	pruned, err = prunePreviousIndexes(ctx, vectors, search, job)
	if err != nil {
		log := logger.From(parent)
		log.Warn().Err(err).Str("document_id", lease.DocumentID).Msg("ready index published; old projection cleanup will retry")
	}
	ix.emit(ctx, evt, "indexed", "")
	return nil
}

func renewSourceLease(
	ctx context.Context, cancel context.CancelFunc, control IndexControl, lease indexdoc.Lease, done chan struct{},
) {
	defer close(done)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			renewal, stop := context.WithTimeout(ctx, 10*time.Second)
			err := control.RenewIndex(renewal, lease)
			stop()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func verifySourceRaw(ctx context.Context, store SnapshotStore, job indexdoc.Job) error {
	reader, err := store.Get(ctx, job.ObjectKey)
	if err != nil {
		return fmt.Errorf("read original source revision: %w", err)
	}
	defer func() { _ = reader.Close() }()
	blake := blake3.New(32, nil)
	sha := sha256.New()
	count, err := io.Copy(io.MultiWriter(blake, sha), io.LimitReader(reader, (64<<20)+1))
	if err != nil {
		return fmt.Errorf("read original bytes: %w", err)
	}
	if count == 0 || count > 64<<20 {
		return errors.New("original exceeds 64 MiB ingestion budget")
	}
	blakeHash, shaHash := hex.EncodeToString(blake.Sum(nil)), hex.EncodeToString(sha.Sum(nil))
	if blakeHash != job.ContentHash {
		return errors.New("original BLAKE3 differs from document")
	}
	if job.Source.Version != 0 && ((job.Source.HashAlgorithm == "sha256" && shaHash != job.Source.ContentHash) ||
		(job.Source.HashAlgorithm == "blake3" && blakeHash != job.Source.ContentHash)) {
		return errors.New("original hash differs from provenance")
	}
	return nil
}

func saveSourceSnapshot(ctx context.Context, store SnapshotStore, text string, snapshot indexdoc.Snapshot,
	job indexdoc.Job) (indexdoc.Manifest, error) {
	manifest := indexdoc.Manifest{
		Version: 1, DocumentID: snapshot.DocumentID, OwnerID: snapshot.OwnerID, SnapshotID: snapshot.SnapshotID,
		SourceRevision: job.Revision(), RawHash: job.RawHash(), RawKey: job.ObjectKey,
		ChunkCount: len(snapshot.Chunks), Model: snapshot.Model, PipelineRevision: snapshot.PipelineRevision,
		ParserRevision: snapshot.ParserRevision,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return manifest, fmt.Errorf("encode source snapshot: %w", err)
	}
	if len(raw) > indexdoc.MaxSnapshotBytes {
		return manifest, errors.New("source snapshot exceeds byte limit")
	}
	prefix := indexdoc.Prefix(snapshot.DocumentID, snapshot.SnapshotID)
	manifest.Text = indexdoc.Object{Key: prefix + snapshot.TextHash + ".txt", SHA256: snapshot.TextHash}
	manifest.Data = indexdoc.Object{Key: prefix + indexdoc.Hash(raw) + ".json", SHA256: indexdoc.Hash(raw)}
	for _, object := range []struct {
		ref  indexdoc.Object
		data []byte
		mime string
	}{
		{manifest.Text, []byte(text), "text/plain; charset=utf-8"}, {manifest.Data, raw, "application/json"},
	} {
		if err = store.Put(ctx, object.ref.Key, bytes.NewReader(object.data), int64(len(object.data)), object.mime); err != nil {
			return manifest, fmt.Errorf("persist source snapshot object: %w", err)
		}
		reader, readErr := store.Get(ctx, object.ref.Key)
		if readErr != nil {
			return manifest, fmt.Errorf("verify source snapshot object: %w", readErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, int64(len(object.data))+1))
		_ = reader.Close()
		if readErr != nil {
			return manifest, fmt.Errorf("verify source snapshot bytes: %w", readErr)
		}
		if indexdoc.Hash(data) != object.ref.SHA256 {
			return manifest, errors.New("source snapshot read-back hash mismatch")
		}
	}
	return manifest, manifest.Validate()
}
