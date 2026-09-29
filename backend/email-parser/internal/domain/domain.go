package domain

import "context"

type TextExtractor interface {
	Extract(ctx context.Context, data []byte) (string, error)
}

type ExtractedEmail struct {
	Text        string
	Author      string
	PublishedAt string
}

type MetaExtractor interface {
	ExtractWithMeta(ctx context.Context, data []byte) (ExtractedEmail, error)
}
