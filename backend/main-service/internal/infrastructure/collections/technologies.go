package collections

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/example/main-service/internal/platform/extractdoc"
	technologyv1 "github.com/example/main-service/internal/platform/genproto/technology/v1"
	"github.com/example/main-service/internal/platform/grpcx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Technologies struct {
	api technologyv1.TechnologyServiceClient
}

func NewTechnologies(addr string) (*Technologies, error) {
	conn, err := grpcx.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("dial technology read plane: %w", err)
	}
	return &Technologies{api: technologyv1.NewTechnologyServiceClient(conn)}, nil
}

func (c *Technologies) ReadRevision(ctx context.Context, in extractdoc.ReadRevision,
) (extractdoc.RevisionTechnologies, error) {
	return technologyCall[extractdoc.ReadRevision, extractdoc.RevisionTechnologies](ctx, in, c.api.ReadRevision)
}

func (c *Technologies) ReadTechnology(ctx context.Context, in extractdoc.ReadTechnology,
) (extractdoc.Technology, error) {
	return technologyCall[extractdoc.ReadTechnology, extractdoc.Technology](ctx, in, c.api.ReadTechnology)
}

func (c *Technologies) Search(ctx context.Context, in extractdoc.SearchTechnologies,
) (extractdoc.TechnologyPage, error) {
	return technologyCall[extractdoc.SearchTechnologies, extractdoc.TechnologyPage](ctx, in, c.api.Search)
}

func (c *Technologies) Review(ctx context.Context, in extractdoc.Review) (extractdoc.Review, error) {
	return technologyCall[extractdoc.Review, extractdoc.Review](ctx, in, c.api.Review)
}

func (c *Technologies) Reviews(ctx context.Context, in extractdoc.ReadReviews) ([]extractdoc.Review, error) {
	return technologyCall[extractdoc.ReadReviews, []extractdoc.Review](ctx, in, c.api.Reviews)
}

func technologyCall[In, Out any](ctx context.Context, in In,
	rpc func(context.Context, *technologyv1.Envelope, ...grpc.CallOption) (*technologyv1.Envelope, error),
) (Out, error) {
	var out Out
	raw, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode technology request: %w", err)
	}
	response, err := rpc(ctx, &technologyv1.Envelope{Json: string(raw)})
	if err != nil {
		return out, technologyError(err)
	}
	return extractdoc.Decode[Out](response.GetJson())
}

// technologyError восстанавливает доменную ошибку из статуса, чтобы об отказе решал HTTP-слой.
func technologyError(err error) error {
	reported := status.Convert(err)
	message := reported.Message()
	if i := strings.LastIndex(message, "desc = "); i >= 0 {
		message = message[i+len("desc = "):]
	}
	switch reported.Code() {
	case codes.InvalidArgument:
		return wrapReason(extractdoc.ErrInvalid, message)
	case codes.NotFound:
		return wrapReason(extractdoc.ErrNotFound, message)
	case codes.Aborted:
		return wrapReason(extractdoc.ErrConflict, message)
	default:
		return fmt.Errorf("technology read plane: %w", err)
	}
}

// wrapReason не повторяет слова сентинела: их уже написала дальняя сторона, а человек не должен
// читать имя ошибки дважды.
func wrapReason(sentinel error, message string) error {
	reason := strings.TrimPrefix(message, sentinel.Error()+": ")
	if reason == "" || reason == message && strings.HasPrefix(message, sentinel.Error()) {
		return sentinel
	}
	return fmt.Errorf("%w: %s", sentinel, reason)
}
