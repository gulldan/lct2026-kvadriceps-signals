package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/example/office-parser/internal/domain"
	"github.com/example/office-parser/internal/platform/contracts"
	"github.com/example/office-parser/internal/platform/ingestion"
	"github.com/example/office-parser/internal/platform/logger"

	commonv1 "github.com/example/office-parser/internal/platform/genproto/common/v1"
)

const source = "office"

// Ревизия привязывает сохранённый текст к экстрактору: локатор одной ревизии не читается против другой.
const parserRevision = "office-extractor-v1"

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
	if p.extractor.IsPlainText(evt.GetFilename(), evt.GetMimeType()) {
		// Простой текст передаётся ссылкой на сам объект: сотни мегабайт не попадают в память процесса.
		parsed.TextObjectKey = evt.GetObjectKey()
		// Хешировать нечего, ревизию фиксирует сам объект; parser_revision всё равно нужен,
		// чтобы локаторы не перешли к следующему экстрактору.
		parsed.Metadata = map[string]string{"parser_revision": parserRevision}
		log.Info().Str("text_object_key", parsed.GetTextObjectKey()).Msg("plain text passed by reference")
	} else {
		result, err := p.extract(ctx, evt)
		if err != nil {
			return p.reporter.Fail(ctx, evt, err)
		}
		parsed.Text = result.Text
		// Хеш текста и ревизия разборщика нужны, чтобы цитату из текста можно было проверить.
		parsed.Metadata = contracts.ParsedMetadata(result.Text, parserRevision, result.Metadata)
		if err := p.storeSidecars(ctx, evt, parsed, result.Sidecars); err != nil {
			return p.reporter.Fail(ctx, evt, err)
		}
	}
	if perr := ingestion.PublishParsed(ctx, p.store, p.pub, parsed); perr != nil {
		return p.reporter.Fail(ctx, evt, perr)
	}
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsed, ""); statusErr != nil {
		return statusErr
	}
	log.Info().Msg("parsed")
	return nil
}

func (p *Processor) extract(ctx context.Context, evt *commonv1.DocumentUploaded) (domain.ExtractionResult, error) {
	data, err := p.store.GetBytes(ctx, evt.GetObjectKey())
	if err != nil {
		return domain.ExtractionResult{}, fmt.Errorf("download %s: %w", evt.GetObjectKey(), err)
	}
	result, err := p.extractor.Extract(ctx, data, evt.GetFilename(), evt.GetMimeType())
	if err != nil {
		return domain.ExtractionResult{}, fmt.Errorf("extract text: %w", err)
	}
	// Пустой результат нельзя помечать проиндексированным: документ потеряется для поиска.
	if strings.TrimSpace(result.Text) == "" {
		return domain.ExtractionResult{}, errors.New("extracted text is empty (no readable text in document)")
	}
	return result, nil
}
