package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func (s *Store) SubmitForReview(ctx context.Context, actor domain.Actor, evidenceID, versionID, expectedRevision int64, requestID string, now time.Time) (domain.EvidenceVersion, error) {
	var saved domain.EvidenceVersion
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		version, err := versionByID(ctx, tx, versionID)
		if err != nil {
			return err
		}
		if evidence.OwnerID != actor.UserID && actor.Role != domain.RoleKnowledgeOwner {
			return apperr.ErrForbidden
		}
		if evidence.Revision != expectedRevision {
			return apperr.ErrVersion
		}
		if version.EvidenceID != evidence.ID || version.State != domain.VersionDraft {
			return apperr.ErrInvalidState
		}
		var claimCount int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM claims WHERE version_id = ?", versionID).Scan(&claimCount); err != nil {
			return normalizeError("count claims before review", err)
		}
		if claimCount == 0 {
			return apperr.New("claims_required", "提交审校前至少需要一条论断", apperr.ErrInvalidState)
		}
		if err := version.State.Transition(domain.VersionUnderReview); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE evidence_versions SET state = ?, revision = revision + 1
            WHERE id = ? AND revision = ?`, domain.VersionUnderReview, versionID, version.Revision)
		if err != nil {
			return normalizeError("submit version for review", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		result, err = tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidenceReviewing, formatTime(now), evidenceID, expectedRevision)
		if err != nil {
			return normalizeError("advance evidence to review", err)
		}
		changed, err = result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "version.review_submitted", ObjectType: "version",
			ObjectID: strconv.FormatInt(versionID, 10), Result: "success", RequestID: requestID,
			Before: map[string]any{"state": version.State, "revision": version.Revision},
			After:  map[string]any{"state": domain.VersionUnderReview, "claim_count": claimCount}, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		saved, err = versionByID(ctx, tx, versionID)
		return err
	})
	return saved, err
}

func (s *Store) ClaimReviewSlot(ctx context.Context, actor domain.Actor, versionID int64, dueAt time.Time, requestID string, now time.Time) (domain.ReviewSlot, error) {
	var slot domain.ReviewSlot
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		version, err := versionByID(ctx, tx, versionID)
		if err != nil {
			return err
		}
		if version.State != domain.VersionUnderReview {
			return apperr.ErrInvalidState
		}
		if version.CreatedBy == actor.UserID {
			return apperr.ErrSelfReview
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO review_slots(version_id, reviewer_id, status, due_at, claimed_at)
            VALUES(?,?,'claimed',?,?)`, versionID, actor.UserID, formatTime(dueAt), formatTime(now))
		if err != nil {
			return fmt.Errorf("claim review slot: %w", normalizeError("reserve review slot", err))
		}
		slotID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read review slot id: %w", err)
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "review.slot_claimed", ObjectType: "version",
			ObjectID: strconv.FormatInt(versionID, 10), Result: "success", RequestID: requestID,
			After: map[string]any{"slot_id": slotID, "reviewer_id": actor.UserID, "due_at": dueAt.UTC()}, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		slot = domain.ReviewSlot{ID: slotID, VersionID: versionID, ReviewerID: actor.UserID,
			Status: "claimed", DueAt: dueAt.UTC(), ClaimedAt: now.UTC()}
		return nil
	})
	return slot, err
}

func (s *Store) DecideReview(ctx context.Context, actor domain.Actor, slotID int64, decision domain.ReviewDecision, opinion, requestID string, now time.Time) (domain.Review, error) {
	var review domain.Review
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var slot domain.ReviewSlot
		var dueAt, claimedAt string
		err := tx.QueryRowContext(ctx, `
            SELECT id, version_id, reviewer_id, status, due_at, claimed_at
            FROM review_slots WHERE id = ?`, slotID).Scan(
			&slot.ID, &slot.VersionID, &slot.ReviewerID, &slot.Status, &dueAt, &claimedAt)
		if err != nil {
			return normalizeError("find review slot", err)
		}
		if slot.ReviewerID != actor.UserID {
			return apperr.ErrForbidden
		}
		if slot.Status != "claimed" {
			return apperr.ErrInvalidState
		}
		slot.DueAt, err = parseTime(dueAt)
		if err != nil {
			return err
		}
		if now.After(slot.DueAt) {
			return apperr.New("review_slot_expired", "审校名额已经过期", apperr.ErrExpired)
		}
		version, err := versionByID(ctx, tx, slot.VersionID)
		if err != nil {
			return err
		}
		if version.CreatedBy == actor.UserID {
			return apperr.ErrSelfReview
		}
		if version.State != domain.VersionUnderReview {
			return apperr.ErrInvalidState
		}
		nextVersion := domain.VersionApproved
		nextEvidence := domain.EvidenceReviewing
		if decision == domain.ReviewRequestChanges {
			nextVersion = domain.VersionDraft
			nextEvidence = domain.EvidenceExtracting
		}
		if err := version.State.Transition(nextVersion); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO reviews(slot_id, version_id, reviewer_id, decision, opinion, created_at)
            VALUES(?,?,?,?,?,?)`, slot.ID, slot.VersionID, actor.UserID, decision, opinion, formatTime(now))
		if err != nil {
			return normalizeError("insert review decision", err)
		}
		reviewID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read review id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE review_slots SET status = 'completed', released_at = ? WHERE id = ? AND status = 'claimed'`,
			formatTime(now), slot.ID); err != nil {
			return normalizeError("complete review slot", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_versions SET state = ?, revision = revision + 1 WHERE id = ? AND revision = ?`,
			nextVersion, version.ID, version.Revision); err != nil {
			return normalizeError("apply review to version", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
			nextEvidence, formatTime(now), version.EvidenceID); err != nil {
			return normalizeError("apply review to evidence", err)
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "review.decided", ObjectType: "version",
			ObjectID: strconv.FormatInt(version.ID, 10), Result: "success", RequestID: requestID,
			Before: map[string]any{"state": version.State, "slot_status": slot.Status},
			After:  map[string]any{"state": nextVersion, "decision": decision, "review_id": reviewID}, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		review = domain.Review{ID: reviewID, SlotID: slot.ID, VersionID: version.ID,
			ReviewerID: actor.UserID, Decision: decision, Opinion: opinion, CreatedAt: now.UTC()}
		return nil
	})
	return review, err
}

func (s *Store) ExpireReviewSlots(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var expired []int64
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
            SELECT id FROM review_slots
            WHERE status = 'claimed' AND due_at <= ? ORDER BY due_at, id LIMIT ?`, formatTime(now), limit)
		if err != nil {
			return normalizeError("find expired review slots", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return normalizeError("scan expired review slot", err)
			}
			expired = append(expired, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range expired {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
                UPDATE review_slots SET status = 'expired', released_at = ?
                WHERE id = ? AND status = 'claimed'`, formatTime(now), id); err != nil {
				return normalizeError("expire review slot", err)
			}
			_, err := AppendAudit(ctx, tx, audit.Payload{Action: "review.slot_expired", ObjectType: "review_slot",
				ObjectID: strconv.FormatInt(id, 10), Result: "success", RequestID: "worker", CreatedAt: now})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return expired, err
}
