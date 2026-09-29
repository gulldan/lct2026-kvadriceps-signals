package dbclient

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/example/email-parser/internal/platform/analysisdoc"
	analysisv1 "github.com/example/email-parser/internal/platform/genproto/analysis/v1"
	"google.golang.org/grpc"
)

func analysisRPC[Out, In any](ctx context.Context, in In,
	call func(context.Context, *analysisv1.Envelope, ...grpc.CallOption) (*analysisv1.Envelope, error),
) (Out, error) {
	var zero Out
	raw, err := json.Marshal(in)
	if err != nil {
		return zero, fmt.Errorf("encode analysis request: %w", err)
	}
	response, err := call(ctx, &analysisv1.Envelope{Json: string(raw)}, grpc.MaxCallRecvMsgSize(analysisdoc.MaxBytes*2))
	if err != nil {
		return zero, fmt.Errorf("analysis RPC: %w", err)
	}
	return analysisdoc.Decode[Out](response.GetJson())
}

func (c *Client) FreezeAnalysis(ctx context.Context, in analysisdoc.Request) (analysisdoc.Frozen, error) {
	return analysisRPC[analysisdoc.Frozen](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).Freeze)
}

func (c *Client) ReadAnalysis(ctx context.Context, in analysisdoc.Read) (analysisdoc.Frozen, error) {
	return analysisRPC[analysisdoc.Frozen](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).Read)
}

func (c *Client) SubmitAnalysis(ctx context.Context, in analysisdoc.Submit) (analysisdoc.Job, error) {
	return analysisRPC[analysisdoc.Job](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).Submit)
}

func (c *Client) ReadAnalysisJob(ctx context.Context, in analysisdoc.JobRead) (analysisdoc.Job, error) {
	return analysisRPC[analysisdoc.Job](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).ReadJob)
}

func (c *Client) StartDescription(ctx context.Context, in analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error) {
	return analysisRPC[analysisdoc.DescriptionJob](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).StartDescription)
}

func (c *Client) ReadDescription(ctx context.Context, in analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error) {
	return analysisRPC[analysisdoc.DescriptionJob](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).ReadDescription)
}

func (c *Client) ClaimDescription(ctx context.Context, in analysisdoc.DescriptionClaim) (analysisdoc.DescriptionClaimed, error) {
	return analysisRPC[analysisdoc.DescriptionClaimed](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).ClaimDescription)
}

func (c *Client) CompleteDescription(
	ctx context.Context, in analysisdoc.DescriptionComplete,
) (analysisdoc.DescriptionJob, error) {
	return analysisRPC[analysisdoc.DescriptionJob](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).CompleteDescription)
}

// Методы живого поиска живут в платформе, а не в копии сервиса: вендоренный файл затирается make vendor.

func (c *Client) ReadLiveSearch(
	ctx context.Context, in analysisdoc.LiveSearchRead,
) (analysisdoc.LiveSearchStored, error) {
	return analysisRPC[analysisdoc.LiveSearchStored](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).ReadLiveSearch)
}

func (c *Client) SaveLiveSearch(
	ctx context.Context, in analysisdoc.LiveSearchSave,
) (analysisdoc.LiveSearchStored, error) {
	return analysisRPC[analysisdoc.LiveSearchStored](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).SaveLiveSearch)
}

func (c *Client) ListLiveSearches(
	ctx context.Context, in analysisdoc.LiveSearchList,
) (analysisdoc.LiveSearchHistory, error) {
	return analysisRPC[analysisdoc.LiveSearchHistory](ctx, in, analysisv1.NewAnalysisServiceClient(c.conn).ListLiveSearches)
}
