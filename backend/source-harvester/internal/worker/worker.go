package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/provider"
)

type Control interface {
	Start(context.Context, harvest.Start) (harvest.Job, error)
	Quota(context.Context, harvest.Quota) (harvest.Grant, error)
	CommitPage(context.Context, harvest.CommitPage) (harvest.Job, error)
	Pending(context.Context, harvest.Pending) (harvest.PendingResult, error)
	Ack(context.Context, harvest.Ack) (harvest.PendingResult, error)
	Finish(context.Context, harvest.Finish) (harvest.Job, error)
	Requested(context.Context, harvest.Requested) ([]harvest.Collection, error)
}

// Objects сохраняет неизменные байты источника до сдвига точки возврата.
type Objects interface {
	Put(context.Context, string, io.Reader, int64, string) error
}

// Publisher возвращается только после подтверждения брокера.
type Publisher interface {
	Publish(context.Context, harvest.Dispatch) error
}

type Worker struct {
	Control   Control
	Objects   Objects
	Publisher Publisher
	Adapter   provider.Adapter
	// BeforeCommit — хук тестового прогона: пишет попытанные ID до изменения базы; манифест в S3 пишется ещё раньше.
	BeforeCommit func(harvest.CommitPage) error
}

func (w *Worker) Run(ctx context.Context, in harvest.Start) (job harvest.Job, runErr error) {
	if err := in.Validate(); err != nil {
		return job, err
	}
	if in.TestRunID != "" && w.BeforeCommit == nil {
		return job, errors.New("test collection requires a pre-commit manifest")
	}
	job, runErr = w.Control.Start(ctx, in)
	if runErr != nil {
		return job, runErr
	}
	defer func() {
		if runErr == nil {
			return
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		// Подробности — только в локальном журнале: ошибка транспорта может нести URL провайдера или ключ хранилища.
		finished, finishErr := w.Control.Finish(
			finishCtx,
			harvest.Finish{
				Lease:       job.Lease,
				Status:      "failed",
				Error:       "collection or dispatch failed; checkpoint retained",
				ResetCursor: errors.Is(runErr, provider.ErrCursorExpired),
			},
		)
		if finishErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("record failed collection outcome: %w", finishErr))
			return
		}
		job = finished
	}()
	if err := provider.SetGate(w.Adapter, &requestGate{control: w.Control, lease: job.Lease}); err != nil {
		return job, err
	}
	if err := w.drain(ctx, job.Lease); err != nil {
		return job, err
	}
	var err error
	job, err = w.collect(ctx, job)
	if err != nil {
		return job, err
	}

	status := "partial"
	if job.Exhausted {
		status = "complete"
	}
	finished, err := w.Control.Finish(ctx, harvest.Finish{Lease: job.Lease, Status: status})
	if err != nil {
		return job, err
	}
	return finished, nil
}

func (w *Worker) drain(ctx context.Context, lease harvest.Lease) error {
	for {
		// Один закреплённый README бывает около 1 МиБ, поэтому ответ контура управления ограничен одной записью.
		pending, err := w.Control.Pending(ctx, harvest.Pending{Lease: lease, Limit: 1})
		if err != nil {
			return err
		}
		if len(pending.Records) == 0 {
			return nil
		}
		for _, record := range pending.Records {
			if err := w.Publisher.Publish(ctx, record); err != nil {
				return fmt.Errorf("dispatch source: %w", err)
			}
			if _, err := w.Control.Ack(ctx, harvest.Ack{Lease: lease, DocumentID: record.DocumentID}); err != nil {
				return err
			}
		}
	}
}

type requestGate struct {
	control Control
	lease   harvest.Lease
}

func (g *requestGate) Before(ctx context.Context) error {
	for {
		grant, err := g.control.Quota(ctx, harvest.Quota{Lease: g.lease, Reserve: true})
		if err != nil {
			return err
		}
		if grant.Allowed {
			return nil
		}
		delay := time.Until(grant.NotBefore)
		if delay > 30*time.Second {
			return fmt.Errorf("provider cooling down until %s", grant.NotBefore.UTC().Format(time.RFC3339))
		}
		if err := wait(ctx, max(10*time.Millisecond, delay)); err != nil {
			return err
		}
	}
}

func (g *requestGate) After(ctx context.Context, at time.Time) error {
	_, err := g.control.Quota(ctx, harvest.Quota{Lease: g.lease, NotBefore: &at})
	return err
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for collection: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (w *Worker) collect(ctx context.Context, job harvest.Job) (harvest.Job, error) {
	scope := provider.Scope(job.Scope)
	var progress provider.PageProgress
	for !job.Exhausted && job.Received < job.Limit {
		page, err := w.Adapter.Fetch(ctx, scope, job.Cursor)
		if err != nil {
			return job, err
		}
		if err = page.Validate(scope.PageSize); err != nil {
			return job, err
		}
		if _, _, err = progress.Observe(&page); err != nil {
			return job, err
		}

		prepared, err := w.prepare(ctx, job, page)
		if err != nil {
			return job, err
		}
		if w.BeforeCommit != nil {
			if err = w.BeforeCommit(prepared); err != nil {
				return job, fmt.Errorf("save test manifest: %w", err)
			}
		}
		next, err := w.Control.CommitPage(ctx, prepared)
		if err != nil {
			return job, err
		}
		job = next
		if err := w.drain(ctx, job.Lease); err != nil {
			return job, err
		}
	}
	return job, nil
}
