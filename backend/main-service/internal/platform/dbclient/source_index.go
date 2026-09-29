package dbclient

import (
	"context"
	"encoding/json/v2"
	"fmt"

	indexdocv1 "github.com/example/main-service/internal/platform/genproto/indexdoc/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"google.golang.org/grpc"
)

func indexRPC[In, Out any](ctx context.Context, in In,
	call func(context.Context, *indexdocv1.Envelope, ...grpc.CallOption) (*indexdocv1.Envelope, error),
) (Out, error) {
	var out Out
	raw, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode index RPC: %w", err)
	}
	response, err := call(ctx, &indexdocv1.Envelope{Json: string(raw)})
	if err != nil {
		return out, fmt.Errorf("source index RPC: %w", err)
	}
	return indexdoc.DecodeResponse[Out](response.GetJson())
}

func (c *Client) BeginIndex(ctx context.Context, in indexdoc.Lease) (indexdoc.Job, error) {
	return indexRPC[indexdoc.Lease, indexdoc.Job](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).Begin)
}

func (c *Client) RenewIndex(ctx context.Context, in indexdoc.Lease) error {
	_, err := indexRPC[indexdoc.Lease, indexdoc.Empty](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).Renew)
	return err
}

func (c *Client) PrepareIndex(ctx context.Context, in indexdoc.Activation) error {
	_, err := indexRPC[indexdoc.Activation, indexdoc.Empty](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).Prepare)
	return err
}

func (c *Client) ActivateIndex(ctx context.Context, in indexdoc.Activation) error {
	_, err := indexRPC[indexdoc.Activation, indexdoc.Empty](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).Activate)
	return err
}

func (c *Client) FinishIndex(ctx context.Context, in indexdoc.Finish) error {
	_, err := indexRPC[indexdoc.Finish, indexdoc.Empty](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).Finish)
	return err
}

func (c *Client) ReadIndexes(ctx context.Context, in indexdoc.Read) (indexdoc.States, error) {
	return indexRPC[indexdoc.Read, indexdoc.States](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).Read)
}

func (c *Client) SetSourceAccess(ctx context.Context, in indexdoc.AccessChange) error {
	_, err := indexRPC[indexdoc.AccessChange, indexdoc.Empty](ctx, in, indexdocv1.NewIndexServiceClient(c.conn).SetAccess)
	return err
}
