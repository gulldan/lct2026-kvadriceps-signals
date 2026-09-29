package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"uuid"

	"lukechampine.com/blake3"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/logger"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

type IngestionService struct {
	sourceIndexes      SourceIndexReader
	sharedCorpus       bool
	requireSourceReady bool
	store              domain.ObjectStore
	docs               domain.DocumentCatalog
	chunks             domain.ChunkReader
}

func NewIngestionService(
	store domain.ObjectStore,
	docs domain.DocumentCatalog,
	chunks domain.ChunkReader,
) *IngestionService {
	return &IngestionService{store: store, docs: docs, chunks: chunks}
}

func (s *IngestionService) Upload(
	ctx context.Context, cmd domain.UploadCommand, mimeType string,
) (*commonv1.Document, bool, error) {
	if cmd.SourceMetadata != "" {
		previous, err := s.existingSource(ctx, cmd)
		if !errors.Is(err, dbclient.ErrNotFound) {
			return previous, previous != nil, err
		}
	}
	docID := uuid.NewV4().String()
	key := fmt.Sprintf("%s/%s/%s", cmd.OwnerID, docID, cmd.Filename)
	size := cmd.Size

	if err := s.store.Put(ctx, key, cmd.Body, size, mimeType); err != nil {
		return nil, false, fmt.Errorf("store object: %w", err)
	}

	return s.FinalizeObject(ctx, FinalizeRequest{
		OwnerID:        cmd.OwnerID,
		Filename:       cmd.Filename,
		MIMEType:       mimeType,
		ObjectKey:      key,
		Size:           size,
		ContentHash:    cmd.Hash,
		InlineText:     cmd.Text,
		ReuseExisting:  cmd.Reuse,
		SourceMetadata: cmd.SourceMetadata,
	})
}

type FinalizeRequest struct {
	OwnerID     string
	Filename    string
	MIMEType    string
	ObjectKey   string
	Size        int64
	ContentHash string
	// InlineText уходит сразу в chunk-splitter мимо разборщиков: мелкий текст читается ещё при приёме.
	InlineText string
	// ReuseExisting: при совпадении хеша вернуть уже проиндексированный документ, а не заводить дубль.
	ReuseExisting bool
	// SourceMetadata записывается атомарно до отправки любой задачи разборщику.
	SourceMetadata string
}

// FinalizeObject: bool — совпадение по хешу с уже проиндексированным содержимым.
func (s *IngestionService) FinalizeObject(
	ctx context.Context, req FinalizeRequest,
) (*commonv1.Document, bool, error) {
	log := logger.From(ctx)

	// Байт в байт такой же документ уже в корпусе: строку владельцу заводим, чтобы загрузка не «пропала», но не
	// индексируем — иначе дубли в выдаче.
	if req.ContentHash != "" && req.SourceMetadata == "" {
		dup, findErr := s.docs.FindDocumentByHash(ctx, req.OwnerID, req.ContentHash)
		if findErr == nil {
			return s.finalizeDuplicate(ctx, req, dup)
		}
		if !errors.Is(findErr, dbclient.ErrNotFound) {
			log.Warn().Err(findErr).Msg("dedup lookup failed; indexing anyway")
		}
	}

	doc, err := s.docs.CreateDocument(ctx, &dbv1.CreateDocumentRequest{
		OwnerId:        req.OwnerID,
		Filename:       req.Filename,
		MimeType:       req.MIMEType,
		Size:           req.Size,
		ObjectKey:      req.ObjectKey,
		ContentHash:    req.ContentHash,
		SourceMetadata: req.SourceMetadata, Enqueue: true, InlineText: req.InlineText,
	})
	if err != nil {
		return nil, false, fmt.Errorf("create document: %w", err)
	}

	log.Info().
		Str("document_id", doc.GetId()).
		Str("mime_type", req.MIMEType).
		Int64("size", req.Size).
		Msg("document queued")
	return doc, false, nil
}

// IngestTextIfNew при совпадении хеша молча пропускает текст, а не заводит строку-дубль, как Upload.
func (s *IngestionService) IngestTextIfNew(ctx context.Context, ownerID, filename, content string) error {
	sum := blake3.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	if _, err := s.docs.FindDocumentByHash(ctx, ownerID, hash); err == nil {
		return nil
	} else if !errors.Is(err, dbclient.ErrNotFound) {
		return err
	}
	_, _, err := s.Upload(ctx, domain.UploadCommand{
		OwnerID:  ownerID,
		Filename: filename,
		Size:     int64(len(content)),
		Body:     strings.NewReader(content),
		Text:     content,
		Hash:     hash,
	}, "text/plain; charset=utf-8")
	return err
}

// RequeueStuck подбирает документы, застрявшие в статусе дольше olderThan: событие брокера могло потеряться.
func (s *IngestionService) RequeueStuck(
	ctx context.Context, status string, olderThan time.Duration, limit int,
) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	n := 0
	err := dbclient.WalkDocuments(ctx, s.docs, "", contracts.DocumentScan{
		Status: status,
	}, func(
		docs []*commonv1.Document,
	) (bool, error) {
		for _, d := range docs {
			if d.GetStatus() != status {
				continue
			}
			if ts, terr := time.Parse(time.RFC3339Nano, d.GetUpdatedAt()); terr == nil && ts.After(cutoff) {
				continue
			}
			if _, derr := s.docs.RequeueDocument(ctx, d.GetId(), max(1, d.GetGeneration()), ""); derr != nil {
				logger.From(ctx).Warn().Err(derr).Str("document_id", d.GetId()).Msg("requeue dispatch failed")
				continue
			}
			n++
			if limit > 0 && n >= limit {
				return false, nil
			}
		}
		return true, nil
	})
	return n, err
}

// RequeueDocuments ставит документы в очередь независимо от статуса — для переразбора после смены парсера.
func (s *IngestionService) RequeueDocuments(ctx context.Context, ids []string) (int, error) {
	n := 0
	for _, id := range ids {
		d, err := s.docs.GetDocument(ctx, id)
		if err != nil {
			return n, err
		}
		if _, err = s.docs.RequeueDocument(ctx, d.GetId(), max(1, d.GetGeneration()), ""); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *IngestionService) ListDocuments(ctx context.Context, ownerID string) ([]*commonv1.Document, error) {
	docs, err := s.docs.ListDocuments(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return s.filterSourceDocuments(ctx, ownerID, docs)
}

func (s *IngestionService) GetDocument(ctx context.Context, ownerID, id string) (*commonv1.Document, error) {
	doc, err := s.docs.GetDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if doc.GetOwnerId() != ownerID {
		return nil, ErrForbidden
	}
	return doc, nil
}

func (s *IngestionService) registerDuplicate(
	ctx context.Context, req FinalizeRequest, original *commonv1.Document,
) (*commonv1.Document, error) {
	doc, err := s.docs.CreateDocument(ctx, &dbv1.CreateDocumentRequest{
		OwnerId:     req.OwnerID,
		Filename:    req.Filename,
		MimeType:    req.MIMEType,
		Size:        req.Size,
		ObjectKey:   req.ObjectKey,
		ContentHash: req.ContentHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create duplicate document: %w", err)
	}
	msg := fmt.Sprintf(
		"дубликат: содержимое уже проиндексировано как «%s»",
		original.GetFilename(),
	)
	if err := s.docs.UpdateDocumentStatus(
		contracts.WithDocumentGeneration(
			ctx,
			doc.GetGeneration(),
		),
		doc.GetId(),
		contracts.StatusIndexed,
		msg,
		nil,
	); err != nil {
		return nil, err
	}
	doc.Status = contracts.StatusIndexed
	doc.StatusMsg = msg
	logger.From(ctx).Info().
		Str("document_id", doc.GetId()).
		Str("duplicate_of", original.GetId()).
		Msg("duplicate upload skipped")
	return doc, nil
}

// DocumentChunks читает чанки от имени владельца документа, а не запрашивающего: так их разложил
// chunk-splitter.
func (s *IngestionService) DocumentChunks(
	ctx context.Context, requesterID string, privileged bool, documentID string,
) (*commonv1.Document, []*llmv1.DocumentChunk, error) {
	doc, err := s.docs.GetDocument(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	access, err := s.sourceAccess(ctx, doc, requesterID, privileged)
	if err != nil {
		return nil, nil, err
	}
	if access.Active != nil {
		chunks, readErr := s.sourceChunks(ctx, access.Active)
		return doc, chunks, readErr
	}
	if s.requireSourceReady && doc.GetSourceMetadata() != "" {
		return doc, []*llmv1.DocumentChunk{}, nil
	}
	chunks, err := s.chunks.DocumentChunks(ctx, doc.GetOwnerId(), documentID)
	if err != nil {
		return nil, nil, fmt.Errorf("document chunks: %w", err)
	}
	if len(chunks) == 0 && doc.GetContentHash() != "" && doc.GetSourceMetadata() == "" {
		orig, derr := s.docs.FindDocumentByHash(ctx, doc.GetOwnerId(), doc.GetContentHash())
		if derr == nil && orig.GetId() != doc.GetId() && orig.GetSourceMetadata() == "" {
			if oc, cerr := s.chunks.DocumentChunks(ctx, orig.GetOwnerId(), orig.GetId()); cerr == nil {
				return doc, oc, nil
			}
		}
	}
	return doc, chunks, nil
}

func (s *IngestionService) OpenDocument(
	ctx context.Context, requesterID string, privileged bool, documentID string,
) (io.ReadCloser, *commonv1.Document, error) {
	doc, err := s.docs.GetDocument(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if _, accessErr := s.sourceAccess(ctx, doc, requesterID, privileged); accessErr != nil {
		return nil, nil, accessErr
	}
	rc, err := s.store.Get(ctx, doc.GetObjectKey())
	if err != nil {
		return nil, nil, fmt.Errorf("open object: %w", err)
	}
	return rc, doc, nil
}

func (s *IngestionService) ListAllDocuments(ctx context.Context, reader string) ([]*commonv1.Document, error) {
	docs, err := s.docs.ListDocuments(ctx, "")
	if err != nil {
		return nil, err
	}
	docs, err = s.filterSourceDocuments(ctx, reader, docs)
	if err != nil {
		return nil, err
	}
	return docs, nil
}

func (s *IngestionService) finalizeDuplicate(
	ctx context.Context, req FinalizeRequest, duplicate *commonv1.Document,
) (*commonv1.Document, bool, error) {
	if duplicate.GetObjectKey() == req.ObjectKey || req.ReuseExisting {
		return duplicate, true, nil
	}
	doc, err := s.registerDuplicate(ctx, req, duplicate)
	return doc, true, err
}
