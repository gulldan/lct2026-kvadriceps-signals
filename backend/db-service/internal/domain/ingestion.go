package domain

import "context"

type IngestionEvent struct {
	ID, DocumentID, Route, Type string
	Generation                  int64
	Payload                     []byte
}

type IngestionRepository interface {
	Queue(context.Context, IngestionEvent) error
	Requeue(context.Context, string, int64, string) (*Document, error)
}
