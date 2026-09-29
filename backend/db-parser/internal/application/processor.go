package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/example/db-parser/internal/domain"
	"github.com/example/db-parser/internal/platform/contracts"
	"github.com/example/db-parser/internal/platform/ingestion"
	"github.com/example/db-parser/internal/platform/logger"

	commonv1 "github.com/example/db-parser/internal/platform/genproto/common/v1"
)

const source = "db"

type ObjectStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

type Processor struct {
	extractor domain.TextExtractor
	store     ObjectStore
	reporter  *ingestion.Reporter
	pub       ingestion.Publisher
}

func New(extractor domain.TextExtractor, store ObjectStore, status ingestion.StatusUpdater, pub ingestion.Publisher) *Processor {
	return &Processor{extractor: extractor, store: store, reporter: ingestion.NewReporter(status, pub), pub: pub}
}

func (p *Processor) Process(ctx context.Context, evt *commonv1.DocumentUploaded) error {
	ctx = contracts.WithDocumentGeneration(ctx, evt.GetGeneration())
	log := logger.From(ctx).With().Str("document_id", evt.GetDocumentId()).Str("source", source).Logger()
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsing, ""); statusErr != nil {
		return statusErr
	}

	parsed := &commonv1.DocumentParsed{
		Generation: evt.GetGeneration(),
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(), ObjectKey: evt.GetObjectKey(),
		Filename: evt.GetFilename(), MimeType: evt.GetMimeType(), Source: source,
		Metadata: nil, TraceId: evt.GetTraceId(),
	}
	data, err := p.store.GetBytes(ctx, evt.GetObjectKey())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("download %s: %w", evt.GetObjectKey(), err))
	}
	text, err := p.extractor.Extract(ctx, data, evt.GetFilename(), evt.GetMimeType())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("extract text: %w", err))
	}
	// Пустой результат — это провал, а не «документ с 0 чанков».
	if strings.TrimSpace(text) == "" {
		return p.reporter.Fail(ctx, evt, errors.New("extracted text is empty (database has no readable rows)"))
	}
	parsed.Text = text
	if perr := ingestion.PublishParsed(ctx, p.store, p.pub, parsed); perr != nil {
		return p.reporter.Fail(ctx, evt, perr)
	}
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsed, ""); statusErr != nil {
		return statusErr
	}
	log.Info().Msg("parsed")
	return nil
}
