package dbclient

import (
	"context"
	dbv1 "github.com/example/email-parser/internal/platform/genproto/db/v1"
	"time"
)

func (c *Client) HypothesisBoard(ctx context.Context, in *dbv1.ListHypothesesRequest) (*dbv1.ListHypothesesResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := c.api.ListHypotheses(ctx, in)
	return out, mapErr(err)
}
