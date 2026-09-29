package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
	"uuid"

	"lukechampine.com/blake3"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/logger"
	"github.com/example/main-service/internal/platform/storage"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// ErrSessionNotFound возвращается и для чужой сессии: ключ включает владельца.
var ErrSessionNotFound = errors.New("upload session not found")

const (
	sessionTTL  = 48 * time.Hour
	partURLTTL  = 2 * time.Hour
	minPartSize = 16 << 20
	// maxParts — с запасом ниже предела S3 в 10 000 частей.
	maxParts     = 9000
	maxPartBatch = 500
)

type UploadSession struct {
	CompletedDocumentID string `json:"completed_document_id,omitempty"`
	CompletedReused     bool   `json:"completed_reused,omitempty"`
	ID                  string `json:"id"`
	OwnerID             string `json:"owner_id"`
	Filename            string `json:"filename"`
	MIMEType            string `json:"mime_type"`
	Size                int64  `json:"size"`
	ObjectKey           string `json:"object_key"`
	S3Upload            string `json:"s3_upload_id"`
	PartSize            int64  `json:"part_size"`
	PartCount           int32  `json:"part_count"`
	Reuse               bool   `json:"reuse,omitempty"`
}

type PartURL struct {
	PartNumber int32
	URL        string
}

// UploadSessionService: браузер грузит части прямо в S3 по подписанным ссылкам, а сессия живёт в Valkey,
// поэтому main-service остаётся без состояния.
type UploadSessionService struct {
	multipart domain.MultipartStore
	objects   domain.ObjectStore
	sessions  domain.SessionStore
	ingestion *IngestionService

	maxBytes int64
}

func NewUploadSessionService(
	multipart domain.MultipartStore,
	objects domain.ObjectStore,
	sessions domain.SessionStore,
	ingestion *IngestionService,
	maxBytes int64,
) *UploadSessionService {
	if maxBytes <= 0 {
		maxBytes = 1 << 30
	}
	return &UploadSessionService{
		multipart: multipart,
		objects:   objects,
		sessions:  sessions,
		ingestion: ingestion,
		maxBytes:  min(maxBytes, 1<<30),
	}
}

func sessionKey(ownerID, id string) string { return "upload:" + ownerID + ":" + id }

func (s *UploadSessionService) Begin(
	ctx context.Context, ownerID, filename, mimeType string, size int64, reuse bool,
) (*UploadSession, error) {
	if size <= 0 {
		return nil, errors.New("size must be positive")
	}
	limit := min(s.maxBytes, int64(64<<20))
	if contracts.RouteForMIME(mimeType, filename) == contracts.RouteParseArch {
		limit = s.maxBytes
	}
	if size > limit {
		return nil, fmt.Errorf("файл больше предела %d МиБ", limit>>20)
	}

	partSize := int64(minPartSize)
	if need := (size + maxParts - 1) / maxParts; need > partSize {
		const step = 8 << 20
		partSize = (need + step - 1) / step * step
	}
	partCount := int32((size + partSize - 1) / partSize)

	id := uuid.NewV4().String()
	key := fmt.Sprintf("uploads/%s/%s/%s", ownerID, id, filename)
	s3id, err := s.multipart.CreateMultipart(ctx, key, mimeType)
	if err != nil {
		return nil, fmt.Errorf("begin multipart: %w", err)
	}

	sess := &UploadSession{
		ID: id, OwnerID: ownerID, Filename: filename, MIMEType: mimeType,
		Size: size, ObjectKey: key, S3Upload: s3id,
		PartSize: partSize, PartCount: partCount, Reuse: reuse,
	}
	if serr := s.sessions.SetJSON(ctx, sessionKey(ownerID, id), sess, sessionTTL); serr != nil {
		_ = s.multipart.AbortMultipart(ctx, key, s3id)
		return nil, fmt.Errorf("persist session: %w", serr)
	}
	logger.From(ctx).Info().
		Str("upload_id", id).Int64("size", size).
		Int64("part_size", partSize).Int32("parts", partCount).
		Msg("upload session opened")
	return sess, nil
}

func (s *UploadSessionService) load(ctx context.Context, ownerID, id string) (*UploadSession, error) {
	var sess UploadSession
	found, err := s.sessions.GetJSON(ctx, sessionKey(ownerID, id), &sess)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if !found {
		return nil, ErrSessionNotFound
	}
	return &sess, nil
}

func (s *UploadSessionService) PartURLs(
	ctx context.Context, ownerID, id string, from, count int32,
) ([]PartURL, error) {
	sess, err := s.load(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if from < 1 || from > sess.PartCount {
		return nil, fmt.Errorf("part %d is out of range 1..%d", from, sess.PartCount)
	}
	if count < 1 {
		count = 1
	}
	if count > maxPartBatch {
		count = maxPartBatch
	}
	if from+count-1 > sess.PartCount {
		count = sess.PartCount - from + 1
	}
	urls := make([]PartURL, 0, count)
	for n := from; n < from+count; n++ {
		u, perr := s.multipart.PresignUploadPart(ctx, sess.ObjectKey, sess.S3Upload, n, partURLTTL)
		if perr != nil {
			return nil, fmt.Errorf("presign part %d: %w", n, perr)
		}
		urls = append(urls, PartURL{PartNumber: n, URL: u})
	}
	return urls, nil
}

func (s *UploadSessionService) Complete(
	ctx context.Context, ownerID, id string, parts []storage.MultipartPart,
) (*commonv1.Document, bool, error) {
	sess, err := s.load(ctx, ownerID, id)
	if err != nil {
		return nil, false, err
	}
	if sess.CompletedDocumentID != "" {
		doc, stepErr := s.ingestion.GetDocument(ctx, ownerID, sess.CompletedDocumentID)
		return doc, sess.CompletedReused, stepErr
	}
	if int32(len(parts)) != sess.PartCount {
		return nil, false, fmt.Errorf("ожидалось %d частей, получено %d", sess.PartCount, len(parts))
	}
	completeErr := s.multipart.CompleteMultipart(ctx, sess.ObjectKey, sess.S3Upload, parts)
	contentHash, hashErr := s.hashObject(ctx, sess.ObjectKey, sess.Size)
	if hashErr != nil {
		return nil, false, errors.Join(completeErr, hashErr)
	}

	doc, existed, err := s.ingestion.FinalizeObject(ctx, FinalizeRequest{
		OwnerID:       ownerID,
		Filename:      sess.Filename,
		MIMEType:      sess.MIMEType,
		ObjectKey:     sess.ObjectKey,
		Size:          sess.Size,
		ContentHash:   contentHash,
		InlineText:    "",
		ReuseExisting: sess.Reuse,
	})
	if err != nil {
		return nil, false, err
	}
	sess.CompletedDocumentID, sess.CompletedReused = doc.GetId(), existed
	if err = s.sessions.SetJSON(ctx, sessionKey(ownerID, id), sess, sessionTTL); err != nil {
		return nil, false, err
	}
	return doc, existed, nil
}

func (s *UploadSessionService) Abort(ctx context.Context, ownerID, id string) error {
	sess, err := s.load(ctx, ownerID, id)
	if errors.Is(err, ErrSessionNotFound) {
		return nil // идемпотентно: отменять нечего
	}
	if err != nil {
		return err
	}
	if aerr := s.multipart.AbortMultipart(ctx, sess.ObjectKey, sess.S3Upload); aerr != nil {
		return aerr
	}
	_ = s.sessions.Del(ctx, sessionKey(ownerID, id))
	return nil
}

func (s *UploadSessionService) hashObject(ctx context.Context, key string, expected int64) (string, error) {
	rc, err := s.objects.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("open object: %w", err)
	}
	defer func() { _ = rc.Close() }()
	hasher := blake3.New(32, nil)
	count, err := io.Copy(hasher, io.LimitReader(rc, min(expected, s.maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("hash object: %w", err)
	}
	if count != expected {
		return "", errors.New("uploaded object size differs from the declared size")
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
