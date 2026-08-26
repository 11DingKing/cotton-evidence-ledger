package publication

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
	store *storage.Store
	now   func() time.Time
}

type CorrectionInput struct {
	EvidenceID         int64  `json:"evidence_id"`
	PublishedVersionID int64  `json:"published_version_id"`
	ExpectedRevision   int64  `json:"expected_revision"`
	Title              string `json:"title"`
	Abstract           string `json:"abstract"`
	ContentHash        string `json:"content_hash"`
}

func New(store *storage.Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = now
	return &copy
}

func (s *Service) Publish(ctx context.Context, actor domain.Actor, evidenceID, versionID, expectedRevision int64, citationTargets []int64) (domain.EvidenceVersion, error) {
	if !actor.Role.CanPublish() {
		return domain.EvidenceVersion{}, apperr.ErrForbidden
	}
	seen := make(map[int64]struct{}, len(citationTargets))
	unique := make([]int64, 0, len(citationTargets))
	for _, targetID := range citationTargets {
		if targetID <= 0 {
			return domain.EvidenceVersion{}, apperr.ErrInvalid
		}
		if _, exists := seen[targetID]; exists {
			continue
		}
		seen[targetID] = struct{}{}
		unique = append(unique, targetID)
	}
	published, err := s.store.Publish(ctx, storage.PublishParams{Actor: actor, EvidenceID: evidenceID,
		VersionID: versionID, ExpectedRevision: expectedRevision, CitationTargets: unique,
		RequestID: audit.RequestID(ctx), Now: s.now().UTC()})
	if err != nil {
		return domain.EvidenceVersion{}, fmt.Errorf("publish evidence version: %w", err)
	}
	return published, nil
}

func (s *Service) StartCorrection(ctx context.Context, actor domain.Actor, input CorrectionInput) (domain.EvidenceVersion, error) {
	if !actor.Role.CanCorrect() {
		return domain.EvidenceVersion{}, apperr.ErrForbidden
	}
	version := domain.EvidenceVersion{Title: strings.TrimSpace(input.Title), Abstract: strings.TrimSpace(input.Abstract),
		ContentHash: strings.TrimSpace(strings.ToLower(input.ContentHash)), CreatedBy: actor.UserID}
	if err := domain.ValidateVersion(version); err != nil {
		return domain.EvidenceVersion{}, err
	}
	created, err := s.store.StartCorrection(ctx, storage.CorrectionParams{Actor: actor,
		EvidenceID: input.EvidenceID, PublishedVersionID: input.PublishedVersionID,
		ExpectedRevision: input.ExpectedRevision, Title: version.Title, Abstract: version.Abstract,
		ContentHash: version.ContentHash, RequestID: audit.RequestID(ctx), Now: s.now().UTC()})
	if err != nil {
		return domain.EvidenceVersion{}, fmt.Errorf("start evidence correction: %w", err)
	}
	return created, nil
}

func (s *Service) Replace(ctx context.Context, actor domain.Actor, evidenceID, oldVersionID, newVersionID, expectedRevision int64) (domain.EvidenceVersion, error) {
	if !actor.Role.CanPublish() {
		return domain.EvidenceVersion{}, apperr.ErrForbidden
	}
	version, err := s.store.ReplacePublishedVersion(ctx, actor, evidenceID, oldVersionID, newVersionID,
		expectedRevision, audit.RequestID(ctx), s.now().UTC())
	if err != nil {
		return domain.EvidenceVersion{}, fmt.Errorf("replace published version: %w", err)
	}
	return version, nil
}

func (s *Service) Withdraw(ctx context.Context, actor domain.Actor, evidenceID, expectedRevision int64, reason string) error {
	if !actor.Role.CanWithdraw() {
		return apperr.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 {
		return apperr.New("invalid_withdrawal_reason", "撤回原因至少需要 8 个字符", apperr.ErrInvalid)
	}
	if err := s.store.WithdrawEvidence(ctx, actor, evidenceID, expectedRevision, reason,
		audit.RequestID(ctx), s.now().UTC()); err != nil {
		return fmt.Errorf("withdraw evidence: %w", err)
	}
	return nil
}

func (s *Service) Archive(ctx context.Context, actor domain.Actor, evidenceID, expectedRevision int64, reason string) error {
	if !actor.Role.CanWithdraw() {
		return apperr.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 {
		return apperr.New("invalid_archive_reason", "归档原因至少需要 8 个字符", apperr.ErrInvalid)
	}
	if err := s.store.ArchiveEvidence(ctx, actor, evidenceID, expectedRevision, reason,
		audit.RequestID(ctx), s.now().UTC()); err != nil {
		return fmt.Errorf("archive withdrawn evidence: %w", err)
	}
	return nil
}

func (s *Service) Restore(ctx context.Context, actor domain.Actor, evidenceID, expectedRevision int64, reason string) error {
	if !actor.Role.CanPublish() {
		return apperr.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 {
		return apperr.New("invalid_restore_reason", "恢复原因至少需要 8 个字符", apperr.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.store.RestoreEvidence(ctx, actor, evidenceID, expectedRevision, reason,
		audit.RequestID(ctx), s.now().UTC()); err != nil {
		return fmt.Errorf("restore evidence for review: %w", err)
	}
	return nil
}

func (s *Service) CreateHandoff(ctx context.Context, actor domain.Actor, evidenceID, toUserID, expectedRevision int64, reason string, expiresAt time.Time) (storage.Handoff, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 || !expiresAt.After(s.now()) || expiresAt.After(s.now().Add(7*24*time.Hour)) {
		return storage.Handoff{}, apperr.ErrInvalid
	}
	handoff, err := s.store.CreateHandoff(ctx, actor, evidenceID, toUserID, expectedRevision, reason,
		audit.RequestID(ctx), expiresAt, s.now().UTC())
	if err != nil {
		return storage.Handoff{}, fmt.Errorf("create responsibility handoff: %w", err)
	}
	return handoff, nil
}

func (s *Service) AcceptHandoff(ctx context.Context, actor domain.Actor, handoffID int64) error {
	if err := s.store.AcceptHandoff(ctx, actor, handoffID, audit.RequestID(ctx), s.now().UTC()); err != nil {
		return fmt.Errorf("accept responsibility handoff: %w", err)
	}
	return nil
}
