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

type Handoff struct {
	ID               int64     `json:"id"`
	EvidenceID       int64     `json:"evidence_id"`
	FromUserID       int64     `json:"from_user_id"`
	ToUserID         int64     `json:"to_user_id"`
	ExpectedRevision int64     `json:"expected_revision"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason"`
	ExpiresAt        time.Time `json:"expires_at"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Store) CreateHandoff(ctx context.Context, actor domain.Actor, evidenceID, toUserID, expectedRevision int64, reason, requestID string, expiresAt, now time.Time) (Handoff, error) {
	var handoff Handoff
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		if evidence.OwnerID != actor.UserID || evidence.Revision != expectedRevision {
			return apperr.ErrVersion
		}
		if toUserID == actor.UserID {
			return apperr.ErrInvalid
		}
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT active FROM users WHERE id = ?", toUserID).Scan(&active); err != nil {
			return normalizeError("find handoff recipient", err)
		}
		if active != 1 {
			return apperr.ErrForbidden
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO responsibility_handoffs(evidence_id, from_user_id, to_user_id,
                expected_revision, status, reason, expires_at, created_at)
            VALUES(?,?,?,?, 'pending', ?,?,?)`, evidenceID, actor.UserID, toUserID,
			expectedRevision, reason, formatTime(expiresAt), formatTime(now))
		if err != nil {
			return normalizeError("create responsibility handoff", err)
		}
		handoff.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read handoff id: %w", err)
		}
		handoff.EvidenceID = evidenceID
		handoff.FromUserID = actor.UserID
		handoff.ToUserID = toUserID
		handoff.ExpectedRevision = expectedRevision
		handoff.Status = "pending"
		handoff.Reason = reason
		handoff.ExpiresAt = expiresAt.UTC()
		handoff.CreatedAt = now.UTC()
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "responsibility.handoff_created",
			ObjectType: "evidence", ObjectID: strconv.FormatInt(evidenceID, 10), Result: "success", RequestID: requestID,
			After: handoff, CreatedAt: now})
		return err
	})
	return handoff, err
}

func (s *Store) AcceptHandoff(ctx context.Context, actor domain.Actor, handoffID int64, requestID string, now time.Time) error {
	return s.InTx(ctx, func(tx *sql.Tx) error {
		var handoff Handoff
		var expires, created string
		err := tx.QueryRowContext(ctx, `
            SELECT id, evidence_id, from_user_id, to_user_id, expected_revision, status, reason, expires_at, created_at
            FROM responsibility_handoffs WHERE id = ?`, handoffID).Scan(&handoff.ID, &handoff.EvidenceID,
			&handoff.FromUserID, &handoff.ToUserID, &handoff.ExpectedRevision, &handoff.Status,
			&handoff.Reason, &expires, &created)
		if err != nil {
			return normalizeError("find responsibility handoff", err)
		}
		if handoff.ToUserID != actor.UserID || handoff.Status != "pending" {
			return apperr.ErrForbidden
		}
		handoff.ExpiresAt, err = parseTime(expires)
		if err != nil {
			return err
		}
		if handoff.ExpiresAt.Before(now.Add(-time.Minute)) {
			return apperr.ErrExpired
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET owner_id = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND owner_id = ? AND revision = ?`, actor.UserID, formatTime(now), handoff.EvidenceID,
			handoff.FromUserID, handoff.ExpectedRevision)
		if err != nil {
			return normalizeError("transfer evidence responsibility", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE responsibility_handoffs SET status = 'accepted', accepted_at = ?
            WHERE id = ? AND status = 'pending'`, formatTime(now), handoff.ID); err != nil {
			return normalizeError("accept responsibility handoff", err)
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "responsibility.handoff_accepted",
			ObjectType: "evidence", ObjectID: strconv.FormatInt(handoff.EvidenceID, 10), Result: "success", RequestID: requestID,
			Before: map[string]any{"owner_id": handoff.FromUserID, "revision": handoff.ExpectedRevision},
			After:  map[string]any{"owner_id": actor.UserID, "revision": handoff.ExpectedRevision + 1}, CreatedAt: now})
		return err
	})
}
