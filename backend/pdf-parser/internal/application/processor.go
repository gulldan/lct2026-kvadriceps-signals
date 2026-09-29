package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/example/pdf-parser/internal/domain"
	"github.com/example/pdf-parser/internal/platform/contracts"
	"github.com/example/pdf-parser/internal/platform/ingestion"
	"github.com/example/pdf-parser/internal/platform/logger"

	commonv1 "github.com/example/pdf-parser/internal/platform/genproto/common/v1"
)

const source = "pdf"

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

	// PDF без текстового слоя — скан: он уходит в OCR, и интерфейс сразу показывает стадию OCR, а не зависший разбор.
	if strings.TrimSpace(text) == "" {
		log.Info().Msg("no text layer, routing to OCR")
		if perr := p.pub.PublishProto(ctx, contracts.ExchangeIngestion, contracts.RouteParseOCR, evt); perr != nil {
			return p.reporter.Fail(ctx, evt, fmt.Errorf("route to ocr: %w", perr))
		}
		if statusErr := p.reporter.Report(ctx, evt, contracts.StatusOCR, "no text layer; routed to OCR"); statusErr != nil {
			return statusErr
		}
		return nil
	}

	parsed := &commonv1.DocumentParsed{
		Generation: evt.GetGeneration(),
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(), ObjectKey: evt.GetObjectKey(),
		Filename: evt.GetFilename(), MimeType: evt.GetMimeType(), Source: source, Text: text,
		Metadata: contracts.ParsedMetadata(text, "pdf-pdfium-v1", documentMetadata(doc)), TraceId: evt.GetTraceId(),
	}
	if perr := ingestion.PublishParsed(ctx, p.store, p.pub, parsed); perr != nil {
		return p.reporter.Fail(ctx, evt, perr)
	}
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsed, ""); statusErr != nil {
		return statusErr
	}
	log.Info().Int("chars", len(text)).Int("pages", len(doc.PageOffsets)).Int("skipped_pages", doc.SkippedPages).Msg("parsed")
	return nil
}

func (p *Processor) extract(ctx context.Context, data []byte) (domain.ExtractedDocument, error) {
	if le, ok := p.extractor.(domain.LayoutExtractor); ok {
		return le.ExtractWithLayout(ctx, data)
	}
	text, err := p.extractor.Extract(ctx, data)
	if err != nil {
		return domain.ExtractedDocument{}, err
	}
	return domain.ExtractedDocument{Text: text}, nil
}

func documentMetadata(doc domain.ExtractedDocument) map[string]string {
	md := pageMetadata(doc)
	if doc.Author == "" && doc.PublishedAt == "" {
		return md
	}
	if md == nil {
		md = map[string]string{}
	}
	md["author"], md["published_at"], md["source_ref"] = doc.Author, doc.PublishedAt, "pdf"
	return md
}

// pageMetadata следует контракту chunk-splitter: page_offsets — смещения в рунах от 0, строго растущие;
// без страниц ключи опускаются целиком, пустой массив недопустим.
func pageMetadata(doc domain.ExtractedDocument) map[string]string {
	if len(doc.PageOffsets) == 0 {
		return nil
	}
	offsets, err := json.Marshal(doc.PageOffsets)
	if err != nil {
		return nil
	}
	md := map[string]string{
		"page_offsets": string(offsets),
		"page_count":   strconv.Itoa(len(doc.PageOffsets)),
	}
	if len(doc.SectionOffsets) > 0 {
		if sections, serr := json.Marshal(doc.SectionOffsets); serr == nil {
			md["section_offsets"] = string(sections)
		}
	}
	return md
}
