package ingestion

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/example/email-parser/internal/platform/contracts"
	"github.com/example/email-parser/internal/platform/logger"

	commonv1 "github.com/example/email-parser/internal/platform/genproto/common/v1"
)

type StatusUpdater interface {
	UpdateDocumentStatus(ctx context.Context, id, status, message string, chunkCount *int32) error
}

type Publisher interface {
	PublishProto(ctx context.Context, exchange, routingKey string, msg proto.Message) error
}

// Reporter сохраняет обязательный статус до уведомления: сбой рассылки не отменяет запись,
// а сбой хранилища оставляет доставку у брокера для повторной обработки.
type Reporter struct {
	status StatusUpdater
	pub    Publisher
}

func NewReporter(status StatusUpdater, pub Publisher) *Reporter {
	return &Reporter{status: status, pub: pub}
}

func (r *Reporter) Report(ctx context.Context, evt *commonv1.DocumentUploaded, status, message string) error {
	if err := r.status.UpdateDocumentStatus(ctx, evt.GetDocumentId(), status, message, nil); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrDurability, err)
	}
	r.Notify(ctx, evt, status, message, 0, 0)
	return nil
}

// Fail показывает окончательный отказ только после исчерпания повторов.
func (r *Reporter) Fail(ctx context.Context, evt *commonv1.DocumentUploaded, err error) error {
	if !contracts.FinalAttempt(ctx) {
		logger.From(ctx).Warn().Err(err).Str("document_id", evt.GetDocumentId()).
			Msg("processing failed; retry pending")
		return err
	}
	logger.From(ctx).Error().Err(err).Str("document_id", evt.GetDocumentId()).Msg("processing failed")
	if statusErr := r.Report(ctx, evt, contracts.StatusFailed, err.Error()); statusErr != nil {
		return statusErr
	}
	return err
}

// Notify сообщает промежуточный прогресс без записи каждого шага в базу.
func (r *Reporter) Notify(
	ctx context.Context, evt *commonv1.DocumentUploaded, status, message string, current, total int32,
) {
	ev := &commonv1.IngestionEvent{
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(),
		Status: status, Message: message, Timestamp: time.Now().UTC().Format(time.RFC3339),
		StageCurrent: current, StageTotal: total,
	}
	if err := r.pub.PublishProto(ctx, contracts.ExchangeEvents, "", ev); err != nil {
		logger.From(ctx).Warn().Err(err).Msg("emit event failed")
	}
}
