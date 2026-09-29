package postgres

import (
	"context"
	"fmt"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/jackc/pgx/v5"
)

// Пилотные потолки ограничивают и хранимые метаданные; перед их повышением нужен архив.
const (
	maxAnalysisInputs      = 2048
	maxOwnerAnalysisInputs = 256
	maxAnalysisJobs        = 4096
	maxOwnerAnalysisJobs   = 512
	maxPendingAnalyses     = 16
	maxOwnerPending        = 2
)

func analysisInputCapacity(ctx context.Context, tx pgx.Tx, request analysisdoc.Request) error {
	var total, owned int
	err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE owner_id=$1 AND
test_run_id=$2) FROM signal_analysis_inputs`,
		request.OwnerID, request.TestRunID).Scan(&total, &owned)
	if err != nil {
		return fmt.Errorf("count saved analysis inputs: %w", err)
	}
	if total >= maxAnalysisInputs || owned >= maxOwnerAnalysisInputs {
		return analysisdoc.ErrHistoryFull
	}
	return nil
}

func analysisJobCapacity(ctx context.Context, tx pgx.Tx, request analysisdoc.Request) error {
	var total, owned, pending, ownerPending int
	err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE i.owner_id=$1 AND
i.test_run_id=$2),
count(*) FILTER (WHERE j.state IN ('prepared','validated')),
count(*) FILTER (WHERE j.state IN ('prepared','validated') AND i.owner_id=$1 AND
i.test_run_id=$2)
FROM signal_analysis_jobs j JOIN signal_analysis_inputs i ON i.run_id=j.input_run_id`,
		request.OwnerID, request.TestRunID).Scan(&total, &owned, &pending, &ownerPending)
	if err != nil {
		return fmt.Errorf("count saved analysis jobs: %w", err)
	}
	if total >= maxAnalysisJobs || owned >= maxOwnerAnalysisJobs {
		return analysisdoc.ErrHistoryFull
	}
	if pending >= maxPendingAnalyses || ownerPending >= maxOwnerPending {
		return analysisdoc.ErrCapacity
	}
	return nil
}
