package application

import (
	"context"
	"github.com/example/db-service/internal/domain"
)

func (s *Service) MessagePage(ctx context.Context, chat, before string, limit int) ([]*domain.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.chats.MessagePage(ctx, chat, before, min(limit, 200))
}
