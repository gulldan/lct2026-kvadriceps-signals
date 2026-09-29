package grpcserver

import (
	"context"
	"encoding/json"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
)

type HypothesisJobs struct {
	dbv1.UnimplementedHypothesisJobsServer
	repo contracts.HypothesisJobStore
}

func NewHypothesisJobs(repo contracts.HypothesisJobStore) *HypothesisJobs {
	return &HypothesisJobs{repo: repo}
}
func encodeJob(job *contracts.HypothesisJob, err error) (*dbv1.HypothesisJobEnvelope, error) {
	if err != nil {
		return nil, toStatus(err)
	}
	if job == nil {
		return &dbv1.HypothesisJobEnvelope{}, nil
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.HypothesisJobEnvelope{Json: string(raw), Token: job.Token}, nil
}
func decodeHypothesisJob(in *dbv1.HypothesisJobEnvelope) (*contracts.HypothesisJob, error) {
	if len(in.GetJson()) > 128<<10 {
		return nil, domain.ErrInvalidArgument
	}
	var job contracts.HypothesisJob
	if err := json.Unmarshal([]byte(in.GetJson()), &job); err != nil {
		return nil, domain.ErrInvalidArgument
	}
	job.Token = in.GetToken()
	return &job, nil
}

func (s *HypothesisJobs) Enqueue(ctx context.Context, in *dbv1.HypothesisJobEnvelope) (*dbv1.HypothesisJobEnvelope, error) {
	job, err := decodeHypothesisJob(in)
	if err != nil {
		return nil, toStatus(err)
	}
	return encodeJob(s.repo.EnqueueHypothesisJob(ctx, job))
}

// Get скрывает токен аренды от читателя состояния.
func (s *HypothesisJobs) Get(ctx context.Context, in *dbv1.GetHypothesisJobRequest) (*dbv1.HypothesisJobEnvelope, error) {
	job, err := s.repo.GetHypothesisJob(ctx, in.GetOwnerId(), in.GetId())
	if job != nil {
		job.Token = ""
	}
	return encodeJob(job, err)
}

func (s *HypothesisJobs) List(ctx context.Context, in *dbv1.ListHypothesisJobsRequest) (*dbv1.ListHypothesisJobsResponse, error) {
	jobs, err := s.repo.ListHypothesisJobs(ctx, in.GetOwnerId(), int(in.GetLimit()))
	if err != nil {
		return nil, toStatus(err)
	}
	out := &dbv1.ListHypothesisJobsResponse{}
	for _, job := range jobs {
		raw, e := json.Marshal(job)
		if e != nil {
			return nil, toStatus(e)
		}
		out.JobsJson = append(out.JobsJson, string(raw))
	}
	return out, nil
}

func (s *HypothesisJobs) Claim(ctx context.Context, _ *dbv1.ClaimHypothesisJobRequest) (*dbv1.HypothesisJobEnvelope, error) {
	return encodeJob(s.repo.ClaimHypothesisJob(ctx))
}

func (s *HypothesisJobs) Renew(ctx context.Context, in *dbv1.HypothesisJobLease) (*dbv1.HypothesisJobEnvelope, error) {
	return encodeJob(nil, s.repo.RenewHypothesisJob(ctx, in.GetId(), in.GetToken()))
}

func (s *HypothesisJobs) Finish(ctx context.Context, in *dbv1.HypothesisJobEnvelope) (*dbv1.HypothesisJobEnvelope, error) {
	job, err := decodeHypothesisJob(in)
	if err != nil {
		return nil, toStatus(err)
	}
	return encodeJob(nil, s.repo.FinishHypothesisJob(ctx, job))
}
