package dbclient

import (
	"context"
	commonv1 "github.com/example/pdf-parser/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/pdf-parser/internal/platform/genproto/db/v1"
	"time"
)

// MessagePage читает ограниченное окно истории в хронологическом порядке.
func (c *Client) MessagePage(ctx context.Context, chat, before string, limit int) ([]*commonv1.Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := c.api.ListMessages(ctx, &dbv1.ListMessagesRequest{ChatId: chat, BeforeId: before, Limit: int32(limit)})
	if err != nil {
		return nil, mapErr(err)
	}
	return out.GetMessages(), nil
}
