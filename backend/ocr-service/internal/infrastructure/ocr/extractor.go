package ocr

import (
	"context"
	"fmt"

	"github.com/example/ocr-service/internal/domain"
	"github.com/example/ocr-service/internal/platform/aiclients"
)

type Extractor struct {
	client aiclients.OCR
}

func NewExtractor(client aiclients.OCR) *Extractor {
	return &Extractor{client: client}
}

func (e *Extractor) Extract(ctx context.Context, data []byte, mime string) (domain.Extraction, error) {
	res, err := e.client.Recognize(ctx, data, mime)
	if err != nil {
		return domain.Extraction{}, fmt.Errorf("ocr recognize: %w", err)
	}
	return domain.Extraction{Text: res.Text, Pages: res.Pages}, nil
}
