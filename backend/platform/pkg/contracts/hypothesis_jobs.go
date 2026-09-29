package contracts

import "context"

type HypothesisJobInput struct {
	HypothesisID   string   `json:"hypothesis_id,omitempty"`
	KPIID          string   `json:"kpi_id,omitempty"`
	KPITitle       string   `json:"kpi_title,omitempty"`
	KPIMetric      string   `json:"kpi_metric,omitempty"`
	KPIDescription string   `json:"kpi_description,omitempty"`
	Constraints    string   `json:"constraints,omitempty"`
	Count          int      `json:"count,omitempty"`
	DocumentIDs    []string `json:"document_ids,omitempty"`
	RequestID      string   `json:"request_id,omitempty"`
}

type HypothesisJob struct {
	ID          string             `json:"id"`
	OwnerID     string             `json:"owner_id"`
	Kind        string             `json:"kind"`
	Status      string             `json:"status"`
	Input       HypothesisJobInput `json:"input"`
	ResultIDs   []string           `json:"result_ids,omitempty"`
	Error       string             `json:"error,omitempty"`
	CreatedAt   string             `json:"created_at"`
	StartedAt   string             `json:"started_at,omitempty"`
	HeartbeatAt string             `json:"heartbeat_at,omitempty"`
	FinishedAt  string             `json:"finished_at,omitempty"`
	Attempt     int                `json:"attempt"`
	Token       string             `json:"-"`
}

type HypothesisJobStore interface {
	EnqueueHypothesisJob(context.Context, *HypothesisJob) (*HypothesisJob, error)
	GetHypothesisJob(context.Context, string, string) (*HypothesisJob, error)
	ListHypothesisJobs(context.Context, string, int) ([]*HypothesisJob, error)
	ClaimHypothesisJob(context.Context) (*HypothesisJob, error)
	RenewHypothesisJob(context.Context, string, string) error
	FinishHypothesisJob(context.Context, *HypothesisJob) error
}

type hypothesisLeaseKey struct{}

// HypothesisLease связывает побочные эффекты с текущим исполнителем.
type HypothesisLease struct{ ID, Token, Step string }

func WithHypothesisLease(ctx context.Context, lease HypothesisLease) context.Context {
	return context.WithValue(ctx, hypothesisLeaseKey{}, lease)
}

func HypothesisLeaseFrom(ctx context.Context) HypothesisLease {
	lease, _ := ctx.Value(hypothesisLeaseKey{}).(HypothesisLease)
	return lease
}
