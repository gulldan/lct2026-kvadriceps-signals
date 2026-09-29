package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/example/vlm-service/internal/domain"
	"github.com/example/vlm-service/internal/platform/contracts"
	"github.com/example/vlm-service/internal/platform/ingestion"
	"github.com/example/vlm-service/internal/platform/logger"

	commonv1 "github.com/example/vlm-service/internal/platform/genproto/common/v1"
)

const source = "vlm"

type ObjectStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

type Processor struct {
	describer domain.ImageDescriber
	store     ObjectStore
	reporter  *ingestion.Reporter
	pub       ingestion.Publisher
}

func New(describer domain.ImageDescriber, store ObjectStore, status ingestion.StatusUpdater, pub ingestion.Publisher) *Processor {
	return &Processor{describer: describer, store: store, reporter: ingestion.NewReporter(status, pub), pub: pub}
}

func (p *Processor) Process(ctx context.Context, evt *commonv1.DocumentUploaded) error {
	ctx = contracts.WithDocumentGeneration(ctx, evt.GetGeneration())
	log := logger.From(ctx).With().Str("document_id", evt.GetDocumentId()).Str("source", source).Logger()
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsing, ""); statusErr != nil {
		return statusErr
	}

	data, err := p.store.GetBytes(ctx, evt.GetObjectKey())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("download %s: %w", evt.GetObjectKey(), err))
	}

	text, err := p.describer.Describe(ctx, data, evt.GetMimeType())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("describe image: %w", err))
	}
	// Пустое описание — это провал, а не «документ с 0 чанков».
	if strings.TrimSpace(text) == "" {
		return p.reporter.Fail(ctx, evt, errors.New("image description is empty"))
	}

	parsed := &commonv1.DocumentParsed{
		Generation: evt.GetGeneration(),
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(), ObjectKey: evt.GetObjectKey(),
		Filename: evt.GetFilename(), MimeType: evt.GetMimeType(), Source: source, Text: text,
		Metadata: nil, TraceId: evt.GetTraceId(),
	}
	if perr := ingestion.PublishParsed(ctx, p.store, p.pub, parsed); perr != nil {
		return p.reporter.Fail(ctx, evt, perr)
	}

	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsed, ""); statusErr != nil {
		return statusErr
	}
	log.Info().Int("chars", len(text)).Msg("described")
	return nil
}
