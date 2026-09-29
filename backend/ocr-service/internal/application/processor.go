package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/example/ocr-service/internal/domain"
	"github.com/example/ocr-service/internal/platform/contracts"
	"github.com/example/ocr-service/internal/platform/ingestion"
	"github.com/example/ocr-service/internal/platform/logger"

	commonv1 "github.com/example/ocr-service/internal/platform/genproto/common/v1"
)

const source = "ocr"

// Два руна: смещения страниц строго растут и через пустые страницы — этого требует chunk-splitter.
const pageSeparator = "\n\n"

const statusOCRMessage = "Распознавание сканов"

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
	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusOCR, statusOCRMessage); statusErr != nil {
		return statusErr
	}

	data, err := p.store.GetBytes(ctx, evt.GetObjectKey())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("download %s: %w", evt.GetObjectKey(), err))
	}

	res, err := p.extractor.Extract(ctx, data, evt.GetMimeType())
	if err != nil {
		return p.reporter.Fail(ctx, evt, fmt.Errorf("recognize text: %w", err))
	}
	text, metadata := pageText(res)
	if pages := len(res.Pages); pages > 0 {
		p.reporter.Notify(ctx, evt, contracts.StatusOCR, statusOCRMessage, int32(pages), int32(pages))
	}

	parsed := &commonv1.DocumentParsed{
		Generation: evt.GetGeneration(),
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(), ObjectKey: evt.GetObjectKey(),
		Filename: evt.GetFilename(), MimeType: evt.GetMimeType(), Source: source, Text: text,
		Metadata: contracts.ParsedMetadata(text, "ocr-pages-v1", metadata), TraceId: evt.GetTraceId(),
	}
	if perr := ingestion.PublishParsed(ctx, p.store, p.pub, parsed); perr != nil {
		return p.reporter.Fail(ctx, evt, perr)
	}

	if statusErr := p.reporter.Report(ctx, evt, contracts.StatusParsed, ""); statusErr != nil {
		return statusErr
	}
	log.Info().Int("chars", len(text)).Int("pages", len(res.Pages)).Msg("parsed")
	return nil
}

// pageText отдаёт page_offsets в рунах; пустые страницы сохраняются, чтобы нумерация совпадала со сканом,
// а смещения считаются в том же проходе склейки, что и текст.
func pageText(res domain.Extraction) (string, map[string]string) {
	if len(res.Pages) == 0 {
		return res.Text, nil
	}
	var b strings.Builder
	offsets := make([]int, 0, len(res.Pages))
	runeLen := 0
	for i, page := range res.Pages {
		if i > 0 {
			b.WriteString(pageSeparator)
			runeLen += utf8.RuneCountInString(pageSeparator)
		}
		offsets = append(offsets, runeLen)
		b.WriteString(page)
		runeLen += utf8.RuneCountInString(page)
	}
	raw, err := json.Marshal(offsets)
	if err != nil {
		return b.String(), nil
	}
	return b.String(), map[string]string{
		"page_offsets": string(raw),
		"page_count":   strconv.Itoa(len(offsets)),
	}
}
