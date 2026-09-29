package application

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func (s *IngestionService) DocumentPage(ctx context.Context, q contracts.DocumentQuery) (*dbv1.DocumentPageResponse, error) {
	return s.docs.DocumentPage(ctx, q)
}

// DocumentStats не переносит отдельные документы в процесс API.
func (s *IngestionService) DocumentStats(ctx context.Context, q contracts.DocumentQuery) (contracts.DocumentStats, error) {
	return s.docs.DocumentStats(ctx, q)
}
