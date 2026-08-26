package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type Handler interface {
	Handle(context.Context, domain.Job) error
}

type Runner struct {
	store    *storage.Store
	handler  Handler
	logger   *slog.Logger
	workerID string
	interval time.Duration
	lease    time.Duration
	now      func() time.Time
	wg       sync.WaitGroup
}

func New(store *storage.Store, handler Handler, logger *slog.Logger, workerID string, interval, lease time.Duration) *Runner {
	return &Runner{store: store, handler: handler, logger: logger, workerID: workerID,
		interval: interval, lease: lease, now: time.Now}
}

func (r *Runner) WithClock(now func() time.Time) *Runner {
	return &Runner{store: r.store, handler: r.handler, logger: r.logger, workerID: r.workerID,
		interval: r.interval, lease: r.lease, now: now}
}

func (r *Runner) Run(ctx context.Context) {
	r.wg.Add(1)
	defer r.wg.Done()
	if recovered, err := r.store.RecoverExpiredLeases(ctx, r.now().UTC()); err != nil {
		r.logger.Error("recover worker leases", "error", err)
	} else if recovered > 0 {
		r.logger.Info("recovered expired worker leases", "count", recovered)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if err := r.processOne(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("worker iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) processOne(ctx context.Context) error {
	now := r.now().UTC()
	job, err := r.store.ClaimJob(ctx, r.workerID, r.lease, now)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("claim worker job: %w", err)
	}
	jobCtx, cancel := context.WithDeadline(ctx, now.Add(r.lease))
	defer cancel()
	err = r.handler.Handle(jobCtx, job)
	if err == nil {
		if completeErr := r.store.CompleteJob(ctx, job.ID, r.workerID, r.now().UTC()); completeErr != nil {
			return fmt.Errorf("complete worker job %d: %w", job.ID, completeErr)
		}
		r.logger.Info("worker job completed", "job_id", job.ID, "kind", job.Kind, "attempt", job.Attempts)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, failErr := r.store.FailJob(ctx, job.ID, r.workerID, err.Error(), r.now().UTC())
	if failErr != nil {
		return fmt.Errorf("record worker job %d failure: %w", job.ID, failErr)
	}
	r.logger.Warn("worker job failed", "job_id", job.ID, "kind", job.Kind,
		"attempt", job.Attempts, "next_status", status, "error", err)
	return nil
}
