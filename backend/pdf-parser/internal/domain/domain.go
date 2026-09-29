package domain

import "context"

type TextExtractor interface {
	Extract(ctx context.Context, data []byte) (string, error)
}

type ExtractedDocument struct {
	Text           string
	PageOffsets    []int
	SectionOffsets []SectionOffset
	// Из словаря документа PDF; PublishedAt — это CreationDate в RFC 3339.
	Author      string
	PublishedAt string
	// SkippedPages — страницы, оставленные пустыми из-за отказа движка на них.
	SkippedPages int
}

type SectionOffset struct {
	Rune    int    `json:"rune"`
	Heading string `json:"heading"`
}

type LayoutExtractor interface {
	ExtractWithLayout(ctx context.Context, data []byte) (ExtractedDocument, error)
}
