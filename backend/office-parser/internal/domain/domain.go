package domain

import "context"

type ExtractionResult struct {
	Text     string
	Metadata map[string]string
	Sidecars []SidecarArtifact
}

// SidecarArtifact — вспомогательный артефакт разбора, например JSON с координатами ячеек книги.
type SidecarArtifact struct {
	Name        string
	ContentType string
	Text        string
}

// TextExtractor: IsPlainText позволяет не скачивать исходник, который уже текст, — на сотнях мегабайт это OOM.
type TextExtractor interface {
	Extract(ctx context.Context, data []byte, filename, mimeType string) (ExtractionResult, error)
	IsPlainText(filename, mimeType string) bool
}
