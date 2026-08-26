package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/jobs"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type Dispatcher struct {
	store   *storage.Store
	reviews *reviews.Service
	now     func() time.Time
}

func NewDispatcher(store *storage.Store, reviewService *reviews.Service) *Dispatcher {
	return &Dispatcher{store: store, reviews: reviewService, now: time.Now}
}

func (d *Dispatcher) Handle(ctx context.Context, job domain.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch job.Kind {
	case jobs.KindIntegrityCheck:
		return d.handleIntegrity(ctx, job)
	case jobs.KindOverdueReview:
		return d.handleOverdue(ctx, job)
	case jobs.KindExpiryReminder:
		return d.handleReminder(ctx, job)
	case jobs.KindImportResume:
		return d.handleImportResume(ctx, job)
	default:
		return fmt.Errorf("unsupported job kind %q", job.Kind)
	}
}

func (d *Dispatcher) handleIntegrity(ctx context.Context, job domain.Job) error {
	if err := d.store.VerifyAuditChain(ctx); err != nil {
		return fmt.Errorf("integrity check for %s/%s: %w", job.ObjectType, job.ObjectID, err)
	}
	return nil
}

func (d *Dispatcher) handleOverdue(ctx context.Context, job domain.Job) error {
	expired, err := d.reviews.ExpireOverdue(ctx, 50)
	if err != nil {
		return err
	}
	for _, slotID := range expired {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, _, err := d.store.CreateNotification(ctx, 1, "overdue-slot-"+strconv.FormatInt(slotID, 10),
			"overdue_review", fmt.Sprintf(`{"slot_id":%d}`, slotID), d.now().UTC())
		if err != nil {
			return fmt.Errorf("notify overdue review slot %d: %w", slotID, err)
		}
	}
	return nil
}

func (d *Dispatcher) handleReminder(ctx context.Context, job domain.Job) error {
	var payload struct {
		UserID  int64  `json:"user_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return fmt.Errorf("decode reminder payload: %w", err)
	}
	if payload.UserID <= 0 || payload.Message == "" {
		return fmt.Errorf("reminder payload is incomplete")
	}
	_, _, err := d.store.CreateNotification(ctx, payload.UserID, "job-"+strconv.FormatInt(job.ID, 10),
		"expiry_reminder", job.Payload, d.now().UTC())
	return err
}

func (d *Dispatcher) handleImportResume(ctx context.Context, job domain.Job) error {
	var payload struct {
		Completed []string `json:"completed"`
		Pending   []string `json:"pending"`
	}
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return fmt.Errorf("decode resumable import payload: %w", err)
	}
	seen := make(map[string]struct{}, len(payload.Completed))
	for _, item := range payload.Completed {
		seen[item] = struct{}{}
	}
	for _, item := range payload.Pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item == "" {
			return fmt.Errorf("pending import item is empty")
		}
		if _, alreadyCompleted := seen[item]; alreadyCompleted {
			continue
		}
		seen[item] = struct{}{}
	}
	return nil
}
