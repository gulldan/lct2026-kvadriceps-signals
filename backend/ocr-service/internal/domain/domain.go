package domain

import "context"

type Extraction struct {
	Text  string
	Pages []string
}

// TextExtractor: пустой результат не считается ошибкой — OCR последний разборщик, дальше отдавать некому.
type TextExtractor interface {
	Extract(ctx context.Context, data []byte, mime string) (Extraction, error)
}
