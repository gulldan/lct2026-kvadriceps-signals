package domain

import "context"

type Entry struct {
	Path          string
	ObjectKey     string
	MIMEType      string
	Size          int64
	NestedArchive bool
	// Hash — BLAKE3-256 в hex (full_hash из archive-scan), по нему идёт дедупликация.
	Hash string
}

type Extraction struct {
	Entries         []Entry
	TotalFiles      int64
	StoredBytes     int64
	Skipped         int
	SkippedOversize int
}

type Extractor interface {
	Extract(ctx context.Context, objectKey, filename, extractionID string) (*Extraction, error)
}
