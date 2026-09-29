package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/example/email-parser/internal/domain"
	"github.com/example/email-parser/internal/platform/contracts"
	"github.com/example/email-parser/internal/platform/ingestion"
	"github.com/example/email-parser/internal/platform/logger"

	commonv1 "github.com/example/email-parser/internal/platform/genproto/common/v1"
)

const source = "email"

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

	data, err := p.store.GetBytes(ctx, evt.GetObjectKey())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("download %s: %w", evt.GetObjectKey(), err))
	}
	doc, err := p.extract(ctx, data)
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("extract text: %w", err))
	}
	text := doc.Text
	// Пустой результат — это провал, а не «документ с 0 чанков».
	if strings.TrimSpace(text) == "" {
		return p.reporter.Fail(ctx, evt, errors.New("extracted text is empty (no readable message body)"))
	}

	parsed := &commonv1.DocumentParsed{
		Generation: evt.GetGeneration(),
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(), ObjectKey: evt.GetObjectKey(),
		Filename: evt.GetFilename(), MimeType: evt.GetMimeType(), Source: source, Text: text,
		Metadata: contracts.ParsedMetadata(text, "email-v1", metaOf(doc)), TraceId: evt.GetTraceId(),
	}
	if perr := ingestion.PublishParsed(ctx, p.store, p.pub, parsed); perr != nil {
		return p.reporter.Fail(ctx, evt, perr)
	}
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsed, ""); statusErr != nil {
		return statusErr
	}
	log.Info().Int("chars", len(text)).Msg("parsed")
	return nil
}

func (p *Processor) extract(ctx context.Context, data []byte) (domain.ExtractedEmail, error) {
	if me, ok := p.extractor.(domain.MetaExtractor); ok {
		return me.ExtractWithMeta(ctx, data)
	}
	text, err := p.extractor.Extract(ctx, data)
	if err != nil {
		return domain.ExtractedEmail{}, err
	}
	return domain.ExtractedEmail{Text: text}, nil
}

func metaOf(doc domain.ExtractedEmail) map[string]string {
	if doc.Author == "" && doc.PublishedAt == "" {
		return nil
	}
	return map[string]string{
		"author":       doc.Author,
		"published_at": doc.PublishedAt,
		"source_ref":   "email",
	}
}
