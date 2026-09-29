// Package collections живёт здесь, а не в общем клиенте БД: коллекции нужны только этому сервису,
// а общий клиент вендорится во все разборщики.
package collections

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"

	harvestv1 "github.com/example/main-service/internal/platform/genproto/harvest/v1"
	"github.com/example/main-service/internal/platform/grpcx"
	"github.com/example/main-service/internal/platform/harvest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Client struct {
	api harvestv1.HarvestServiceClient
}

func New(addr string) (*Client, error) {
	conn, err := grpcx.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("dial collection control plane: %w", err)
	}
	return &Client{api: harvestv1.NewHarvestServiceClient(conn)}, nil
}

func call[In, Out any](ctx context.Context, in In,
	rpc func(context.Context, *harvestv1.Envelope, ...grpc.CallOption) (*harvestv1.Envelope, error),
) (Out, error) {
	var out Out
	raw, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode collection request: %w", err)
	}
	response, err := rpc(ctx, &harvestv1.Envelope{Json: string(raw)})
	if err != nil {
		return out, controlPlaneError(err)
	}
	return harvest.Decode[Out](response.GetJson())
}

// controlPlaneError восстанавливает доменную ошибку из статуса: об отказе решает HTTP-слой,
// и gRPC-кадр клиенту не пересказывается.
func controlPlaneError(err error) error {
	reported := status.Convert(err)
	message := reported.Message()
	if i := strings.LastIndex(message, "desc = "); i >= 0 {
		message = message[i+len("desc = "):]
	}
	switch reported.Code() {
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", harvest.ErrInvalid, message)
	case codes.Aborted:
		return fmt.Errorf("%w: %s", harvest.ErrConflict, message)
	case codes.NotFound:
		return fmt.Errorf("%w: %s", harvest.ErrNotFound, message)
	default:
		return fmt.Errorf("collection control plane: %w", err)
	}
}

// Request идемпотентен по request_id: тот же ввод даёт ту же коллекцию, другой ввод под тем же id
// отклоняется, а не подменяет её.
func (c *Client) Request(ctx context.Context, in harvest.Request) (harvest.Collection, error) {
	return call[harvest.Request, harvest.Collection](ctx, in, c.api.Request)
}

func (c *Client) Progress(ctx context.Context, in harvest.Progress) (harvest.Collection, error) {
	return call[harvest.Progress, harvest.Collection](ctx, in, c.api.Progress)
}
