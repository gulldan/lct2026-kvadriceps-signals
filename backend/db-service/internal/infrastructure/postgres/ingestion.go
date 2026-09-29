package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/example/db-service/internal/platform/contracts"
	commonv1 "github.com/example/db-service/internal/platform/genproto/common/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

func (r *DocumentRepo) createWithIngestion(ctx context.Context, d *domain.Document) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("create with ingestion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := tx.Exec(ctx, `INSERT INTO documents
 (id,owner_id,filename,mime_type,size,object_key,status,status_msg,chunk_count,created_at,updated_at,content_hash,source_metadata)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(id) DO NOTHING`,
		d.ID, d.OwnerID, d.Filename, d.MIMEType, d.Size, d.ObjectKey, d.Status, d.StatusMsg, d.ChunkCount, d.CreatedAt, d.UpdatedAt,
		d.ContentHash, d.SourceMetadata)
	if err != nil {
		return fmt.Errorf("create document and ingestion: %w", err)
	}
	stored, err := sqlcgen.New(tx).GetDocument(ctx, d.ID)
	if err != nil {
		return err
	}
	if stored.OwnerID != d.OwnerID || stored.Filename != d.Filename || stored.MimeType != d.MIMEType ||
		stored.Size != d.Size || stored.ContentHash != d.ContentHash || !jsonEqual(stored.SourceMetadata, d.SourceMetadata) {
		return domain.ErrConflict
	}
	if d.Enqueue && created.RowsAffected() == 1 {
		event, encodeErr := uploadEvent(documentFromRow(stored), d.InlineText, d.ArchiveDepth)
		if encodeErr != nil {
			return encodeErr
		}
		if err = insertIngestion(ctx, tx, event); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("create with ingestion: %w", err)
	}
	*d = *documentFromRow(stored)
	return nil
}

func uploadEvent(d *domain.Document, text string, depth int32) (domain.IngestionEvent, error) {
	var event proto.Message
	route := contracts.RouteForMIME(d.MIMEType, d.Filename)
	if text != "" && (strings.HasPrefix(d.MIMEType, "text/plain") || strings.HasPrefix(d.MIMEType, "text/html")) {
		source := "text"
		if strings.HasPrefix(d.MIMEType, "text/html") {
			source = "html"
		}
		route = contracts.RouteChunk
		event = &commonv1.DocumentParsed{DocumentId: d.ID, OwnerId: d.OwnerID, ObjectKey: d.ObjectKey, Filename: d.Filename,
			MimeType: d.MIMEType, Text: text, Source: source, Generation: d.Generation,
			Metadata: contracts.ParsedMetadata(text, "text-html-v1", nil)}
	} else {
		event = &commonv1.DocumentUploaded{DocumentId: d.ID, OwnerId: d.OwnerID, ObjectKey: d.ObjectKey, Filename: d.Filename,
			MimeType: d.MIMEType, Size: d.Size, ArchiveDepth: depth, Generation: d.Generation}
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		return domain.IngestionEvent{}, fmt.Errorf("encode ingestion event: %w", err)
	}
	return domain.IngestionEvent{ID: d.ID + "/" + strconv.FormatInt(d.Generation, 10) + "/initial",
		DocumentID: d.ID, Generation: d.Generation, Route: route, Type: string(
			event.ProtoReflect().Descriptor().FullName(),
		), Payload: payload}, nil
}

func insertIngestion(ctx context.Context, tx pgx.Tx, event domain.IngestionEvent) error {
	tag, err := tx.Exec(ctx, `INSERT INTO ingestion_outbox(id,document_id,generation,route,event_type,payload)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET id=ingestion_outbox.id
 WHERE ingestion_outbox.route=EXCLUDED.route AND ingestion_outbox.payload=EXCLUDED.payload`,
		event.ID, event.DocumentID, event.Generation, event.Route, event.Type, event.Payload)
	if err != nil {
		return fmt.Errorf("persist ingestion event: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

// Queue фиксирует следующую стадию до подтверждения входной доставки.
func (r *DocumentRepo) Queue(ctx context.Context, event domain.IngestionEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var generation int64
	if err = tx.QueryRow(
		ctx,
		`SELECT generation FROM documents WHERE id=$1 FOR UPDATE`,
		event.DocumentID,
	).Scan(
		&generation,
	); err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	if generation != event.Generation {
		return nil
	}
	next := contracts.StatusParsing
	if event.Route == contracts.RouteChunk {
		next = contracts.StatusParsed
	}
	if event.Route == contracts.RouteParseOCR {
		next = contracts.StatusOCR
	}
	if _, err = sqlcgen.New(tx).UpdateDocumentStatus(ctx, sqlcgen.UpdateDocumentStatusParams{
		ID: event.DocumentID, Status: next, Generation: event.Generation,
	}); err != nil {
		return err
	}
	if err = insertIngestion(ctx, tx, event); err != nil {
		return err
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return fmt.Errorf("queue: %w", operationErr)
	}
	return nil
}

// Requeue создаёт новое поколение и его отправку одной транзакцией.
func (r *DocumentRepo) Requeue(ctx context.Context, id string, expected int64, inline string) (*domain.Document, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("requeue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM documents WHERE id=$1 FOR UPDATE`, id).Scan(&generation); err != nil {
		return nil, fmt.Errorf("requeue: %w", err)
	}
	if generation != expected {
		return nil, domain.ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE documents SET generation=generation+1,status='queued',status_msg='',updated_at=now()
 WHERE id=$1`, id); err != nil {
		return nil, fmt.Errorf("requeue: %w", err)
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE source_index_heads SET lease_token='',leased_until='-infinity' WHERE document_id=$1`,
		id,
	); err != nil {
		return nil, fmt.Errorf("requeue: %w", err)
	}
	row, err := sqlcgen.New(tx).GetDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	d := documentFromRow(row)
	event, err := uploadEvent(d, inline, 0)
	if err != nil {
		return nil, err
	}
	if err = insertIngestion(ctx, tx, event); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("requeue: %w", err)
	}
	return d, nil
}

// DispatchOne держит только одну строку; потеря COMMIT допускает безопасную повторную доставку.
func (r *DocumentRepo) DispatchOne(
	ctx context.Context,
	publish func(
		context.Context,
		string,
		proto.Message,
	) error,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("dispatch one: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, route, kind string
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT id,route,event_type,payload FROM ingestion_outbox WHERE published_at IS NULL
 ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &route, &kind, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dispatch one: %w", err)
	}
	var message proto.Message
	switch kind {
	case "rag.common.v1.DocumentUploaded":
		message = &commonv1.DocumentUploaded{}
	case "rag.common.v1.DocumentParsed":
		message = &commonv1.DocumentParsed{}
	default:
		return false, fmt.Errorf("unknown stored ingestion type %q", kind)
	}
	if err = proto.Unmarshal(payload, message); err != nil {
		return false, fmt.Errorf("dispatch one: %w", err)
	}
	if err = publish(ctx, route, message); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE ingestion_outbox SET published_at=now() WHERE id=$1`, id); err != nil {
		return false, fmt.Errorf("dispatch one: %w", err)
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return true, fmt.Errorf("dispatch one: %w", operationErr)
	}
	return true, nil
}

func jsonEqual(a, b []byte) bool {
	// PostgreSQL нормализует JSON; сравнение структуры допускает разный порядок ключей.
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
