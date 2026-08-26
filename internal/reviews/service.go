package reviews

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type Service struct {
	store      *storage.Store
	now        func() time.Time
	defaultDue time.Duration
	maximumDue time.Duration
}

func New(store *storage.Store) *Service {
	return &Service{store: store, now: time.Now, defaultDue: 72 * time.Hour, maximumDue: 14 * 24 * time.Hour}
}

func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = now
	return &copy
}

func (s *Service) Submit(ctx context.Context, actor domain.Actor, evidenceID, versionID, expectedRevision int64) (domain.EvidenceVersion, error) {
	if actor.Role != domain.RoleResearcher && actor.Role != domain.RoleKnowledgeOwner {
		return domain.EvidenceVersion{}, apperr.ErrForbidden
	}
	version, err := s.store.SubmitForReview(ctx, actor, evidenceID, versionID, expectedRevision,
		audit.RequestID(ctx), s.now().UTC())
	if err != nil {
		return domain.EvidenceVersion{}, fmt.Errorf("submit version for cross review: %w", err)
	}
	return version, nil
}

func (s *Service) Claim(ctx context.Context, actor domain.Actor, versionID int64, dueAt *time.Time) (domain.ReviewSlot, error) {
	if !actor.Role.CanReview() {
		return domain.ReviewSlot{}, apperr.ErrForbidden
	}
	now := s.now().UTC()
	due := now.Add(s.defaultDue)
	if dueAt != nil {
		due = dueAt.UTC()
	}
	if !due.After(now) || due.After(now.Add(s.maximumDue)) {
		return domain.ReviewSlot{}, apperr.New("invalid_review_due", "审校截止时间必须位于未来 14 天内", apperr.ErrInvalid)
	}
	slot, err := s.store.ClaimReviewSlot(ctx, actor, versionID, due, audit.RequestID(ctx), now)
	if err != nil {
		return domain.ReviewSlot{}, fmt.Errorf("claim review capacity: %w", err)
	}
	return slot, nil
}

func (s *Service) Decide(ctx context.Context, actor domain.Actor, slotID int64, decision domain.ReviewDecision, opinion string) (domain.Review, error) {
	if !actor.Role.CanReview() {
		return domain.Review{}, apperr.ErrForbidden
	}
	opinion = strings.TrimSpace(opinion)
	if err := domain.ValidateReview(decision, opinion); err != nil {
		return domain.Review{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Review{}, err
	}
	review, err := s.store.DecideReview(context.Background(), actor, slotID, decision, opinion,
		audit.RequestID(ctx), s.now().UTC())
	if err != nil {
		return domain.Review{}, fmt.Errorf("persist cross review decision: %w", err)
	}
	return review, nil
}

func (s *Service) ExpireOverdue(ctx context.Context, limit int) ([]int64, error) {
	expired, err := s.store.ExpireReviewSlots(ctx, s.now().UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("expire overdue reviews: %w", err)
	}
	return expired, nil
}
