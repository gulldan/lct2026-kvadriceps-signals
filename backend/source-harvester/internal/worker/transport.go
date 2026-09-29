package worker

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/example/source-harvester/internal/platform/contracts"
	commonv1 "github.com/example/source-harvester/internal/platform/genproto/common/v1"
	harvestv1 "github.com/example/source-harvester/internal/platform/genproto/harvest/v1"
	"github.com/example/source-harvester/internal/platform/harvest"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	RPC harvestv1.HarvestServiceClient
}

func rpc[In, Out any](
	ctx context.Context, in In,
	call func(context.Context, *harvestv1.Envelope, ...grpc.CallOption) (*harvestv1.Envelope, error),
) (Out, error) {
	var zero Out
	raw, err := json.Marshal(in)
	if err != nil {
		return zero, fmt.Errorf("collection RPC: %w", err)
	}
	out, err := call(ctx, &harvestv1.Envelope{Json: string(raw)})
	if err != nil {
		return zero, fmt.Errorf("collection RPC: %w", err)
	}
	return harvest.Decode[Out](out.GetJson())
}

func (c Client) Start(ctx context.Context, in harvest.Start) (harvest.Job, error) {
	return rpc[harvest.Start, harvest.Job](ctx, in, c.RPC.Start)
}

func (c Client) Requested(ctx context.Context, in harvest.Requested) ([]harvest.Collection, error) {
	return rpc[harvest.Requested, []harvest.Collection](ctx, in, c.RPC.Requested)
}

func (c Client) Quota(ctx context.Context, in harvest.Quota) (harvest.Grant, error) {
	return rpc[harvest.Quota, harvest.Grant](ctx, in, c.RPC.Quota)
}

func (c Client) CommitPage(ctx context.Context, in harvest.CommitPage) (harvest.Job, error) {
	return rpc[harvest.CommitPage, harvest.Job](ctx, in, c.RPC.CommitPage)
}

func (c Client) Pending(ctx context.Context, in harvest.Pending) (harvest.PendingResult, error) {
	return rpc[harvest.Pending, harvest.PendingResult](ctx, in, c.RPC.Pending)
}

func (c Client) Ack(ctx context.Context, in harvest.Ack) (harvest.PendingResult, error) {
	return rpc[harvest.Ack, harvest.PendingResult](ctx, in, c.RPC.Ack)
}

func (c Client) Finish(ctx context.Context, in harvest.Finish) (harvest.Job, error) {
	return rpc[harvest.Finish, harvest.Job](ctx, in, c.RPC.Finish)
}

type ConfirmedPublisher interface {
	PublishProto(context.Context, string, string, proto.Message) error
}

type Ingestion struct{ Publisher ConfirmedPublisher }

func (p Ingestion) Publish(ctx context.Context, in harvest.Dispatch) error {
	if in.MIME == mimeText {
		return p.Publisher.PublishProto(
			ctx,
			contracts.ExchangeIngestion,
			contracts.RouteChunk,
			&commonv1.DocumentParsed{
				DocumentId: in.DocumentID,
				OwnerId:    in.OwnerID,
				ObjectKey:  in.ObjectKey,
				Filename:   in.Filename,
				MimeType:   in.MIME,
				Source:     "text",
				Text:       in.Text,
				Metadata:   contracts.ParsedMetadata(in.Text, "provider-text-v1", nil),
				TraceId:    in.DocumentID,
			},
		)
	}
	return p.Publisher.PublishProto(
		ctx,
		contracts.ExchangeIngestion,
		contracts.RouteForMIME(in.MIME, in.Filename),
		&commonv1.DocumentUploaded{
			DocumentId: in.DocumentID,
			OwnerId:    in.OwnerID,
			ObjectKey:  in.ObjectKey,
			Filename:   in.Filename,
			MimeType:   in.MIME,
			Size:       in.Size,
			TraceId:    in.DocumentID,
		},
	)
}
