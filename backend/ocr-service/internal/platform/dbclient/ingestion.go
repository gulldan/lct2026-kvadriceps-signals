package dbclient

import (
	"context"
	"fmt"
	"time"

	"github.com/example/ocr-service/internal/platform/contracts"
	commonv1 "github.com/example/ocr-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/ocr-service/internal/platform/genproto/db/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// PublishIngestion подтверждает событие только после фиксации в PostgreSQL.
func (c *Client) PublishIngestion(ctx context.Context, route string, event proto.Message) error {
	request := &dbv1.QueueDocumentEventRequest{Route: route}
	switch value := event.(type) {
	case *commonv1.DocumentUploaded:
		request.Event = &dbv1.QueueDocumentEventRequest_Uploaded{Uploaded: value}
	case *commonv1.DocumentParsed:
		request.Event = &dbv1.QueueDocumentEventRequest_Parsed{Parsed: value}
	default:
		return fmt.Errorf("unsupported ingestion event %T", event)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := dbv1.NewIngestionControlClient(c.conn).QueueEvent(ctx, request)
	if status.Code(err) == codes.Aborted || status.Code(err) == codes.InvalidArgument {
		return contracts.Unrecoverable(err)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrDurability, err)
	}
	return nil
}

// RequeueDocument ограждает новую обработку от старых доставок.
func (c *Client) RequeueDocument(ctx context.Context, id string, expected int64, inline string) (*commonv1.Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	doc, err := dbv1.NewIngestionControlClient(
		c.conn,
	).Requeue(
		ctx,
		&dbv1.RequeueDocumentRequest{
			Id:                 id,
			ExpectedGeneration: expected,
			InlineText:         inline,
		},
	)
	return doc, mapErr(err)
}
