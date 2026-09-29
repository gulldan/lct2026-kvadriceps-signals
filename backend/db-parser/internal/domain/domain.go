package domain

import "context"

type TextExtractor interface {
	Extract(ctx context.Context, data []byte, filename, mimeType string) (string, error)
}
