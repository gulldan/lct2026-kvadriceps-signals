package dbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/office-parser/internal/platform/contracts"
	dbv1 "github.com/example/office-parser/internal/platform/genproto/db/v1"
)

func jobEnvelope(job *contracts.HypothesisJob) (*dbv1.HypothesisJobEnvelope, error) {
	raw, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("encode hypothesis job: %w", err)
	}
	return &dbv1.HypothesisJobEnvelope{Json: string(raw), Token: job.Token}, nil
}
func decodeJob(in *dbv1.HypothesisJobEnvelope, err error) (*contracts.HypothesisJob, error) {
	if err != nil {
		return nil, mapErr(err)
	}
	if in.GetJson() == "" {
		return nil, ErrNotFound
	}
	var out contracts.HypothesisJob
	if err = json.Unmarshal([]byte(in.GetJson()), &out); err != nil {
		return nil, fmt.Errorf("hypothesis job: %w", err)
	}
	out.Token = in.GetToken()
	return &out, nil
}

func (c *Client) EnqueueHypothesisJob(ctx context.Context, in *contracts.HypothesisJob) (*contracts.HypothesisJob, error) {
	req, err := jobEnvelope(in)
	if err != nil {
		return nil, fmt.Errorf("hypothesis job: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return decodeJob(dbv1.NewHypothesisJobsClient(c.conn).Enqueue(ctx, req))
}

func (c *Client) GetHypothesisJob(ctx context.Context, owner, id string) (*contracts.HypothesisJob, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return decodeJob(dbv1.NewHypothesisJobsClient(c.conn).Get(ctx, &dbv1.GetHypothesisJobRequest{OwnerId: owner, Id: id}))
}

func (c *Client) ListHypothesisJobs(ctx context.Context, owner string, limit int) ([]*contracts.HypothesisJob, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	response, err := dbv1.NewHypothesisJobsClient(
		c.conn,
	).List(
		ctx,
		&dbv1.ListHypothesisJobsRequest{
			OwnerId: owner,
			Limit: int32(
				limit,
			),
		},
	)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*contracts.HypothesisJob, 0, len(response.GetJobsJson()))
	for _, raw := range response.GetJobsJson() {
		var j contracts.HypothesisJob
		if err = json.Unmarshal([]byte(raw), &j); err != nil {
			return nil, fmt.Errorf("hypothesis job: %w", err)
		}
		out = append(out, &j)
	}
	return out, nil
}

func (c *Client) ClaimHypothesisJob(ctx context.Context) (*contracts.HypothesisJob, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return decodeJob(dbv1.NewHypothesisJobsClient(c.conn).Claim(ctx, &dbv1.ClaimHypothesisJobRequest{}))
}

func (c *Client) RenewHypothesisJob(ctx context.Context, id, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := dbv1.NewHypothesisJobsClient(c.conn).Renew(ctx, &dbv1.HypothesisJobLease{Id: id, Token: token})
	return mapErr(err)
}

func (c *Client) FinishHypothesisJob(ctx context.Context, in *contracts.HypothesisJob) error {
	req, err := jobEnvelope(in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = dbv1.NewHypothesisJobsClient(c.conn).Finish(ctx, req)
	return mapErr(err)
}
