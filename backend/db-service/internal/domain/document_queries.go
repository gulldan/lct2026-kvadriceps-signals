package domain

import (
	"context"
	"github.com/example/db-service/internal/platform/contracts"
)

type DocumentPage struct {
	Documents  []*Document
	Total      int64
	NextCursor string
}

type DocumentQueries interface {
	Page(context.Context, contracts.DocumentQuery) (DocumentPage, error)
	Stats(context.Context, contracts.DocumentQuery) (contracts.DocumentStats, error)
}
