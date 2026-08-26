package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

const (
	KindIntegrityCheck = "integrity_check"
	KindOverdueReview  = "overdue_review"
	KindExpiryReminder = "expiry_reminder"
	KindImportResume   = "import_resume"
)

type Service struct {
	store *storage.Store
	now   func() time.Time
}

func New(store *storage.Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = now
	return &copy
}

func (s *Service) Enqueue(ctx context.Context, actor domain.Actor, kind, objectType string, objectID int64, payload any) (domain.Job, error) {
	if actor.Role != domain.RoleKnowledgeOwner && kind != KindImportResume {
		return domain.Job{}, apperr.ErrForbidden
	}
	if !validKind(kind) || objectType == "" || objectID <= 0 {
		return domain.Job{}, apperr.ErrInvalid
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job payload: %w", err)
	}
	job, err := s.store.EnqueueJob(ctx, domain.Job{Kind: kind, ObjectType: objectType,
		ObjectID: strconv.FormatInt(objectID, 10), Payload: string(encoded), MaxAttempts: 5}, s.now().UTC())
	if err != nil {
		return domain.Job{}, fmt.Errorf("enqueue background job: %w", err)
	}
	return job, nil
}

func (s *Service) Counts(ctx context.Context, actor domain.Actor) (map[string]int, error) {
	if actor.Role != domain.RoleKnowledgeOwner {
		return nil, apperr.ErrForbidden
	}
	return s.store.JobCounts(ctx)
}

func validKind(kind string) bool {
	switch kind {
	case KindIntegrityCheck, KindOverdueReview, KindExpiryReminder, KindImportResume:
		return true
	default:
		return false
	}
}
