package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func (s *Store) CreateSessionAudited(ctx context.Context, userID int64, tokenHash, requestID string, expiresAt, now time.Time) (int64, error) {
	var sessionID int64
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
            INSERT INTO sessions(user_id, token_hash, expires_at, created_at, last_seen_at)
            VALUES(?,?,?,?,?)`, userID, tokenHash, formatTime(expiresAt), formatTime(now), formatTime(now))
		if err != nil {
			return normalizeError("create audited session", err)
		}
		sessionID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read audited session id: %w", err)
		}
		actorID := userID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "session.logged_in",
			ObjectType: "session", ObjectID: fmt.Sprint(sessionID), Result: "success", RequestID: requestID,
			After: map[string]any{"expires_at": expiresAt.UTC()}, CreatedAt: now})
		return err
	})
	return sessionID, err
}

func (s *Store) RevokeSessionAudited(ctx context.Context, actor domain.Actor, requestID string, now time.Time) error {
	return s.InTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
            UPDATE sessions SET revoked_at = ?
            WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, formatTime(now), actor.SessionID, actor.UserID)
		if err != nil {
			return normalizeError("revoke audited session", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read audited revoke count: %w", err)
		}
		if count != 1 {
			return apperr.ErrUnauthorized
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "session.logged_out",
			ObjectType: "session", ObjectID: fmt.Sprint(actor.SessionID), Result: "success", RequestID: requestID,
			Before: map[string]any{"revoked": false}, After: map[string]any{"revoked": true}, CreatedAt: now})
		return err
	})
}

func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
        INSERT INTO sessions(user_id, token_hash, expires_at, created_at, last_seen_at)
        VALUES(?,?,?,?,?)`, userID, tokenHash, formatTime(expiresAt), formatTime(now), formatTime(now))
	if err != nil {
		return 0, normalizeError("create session", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read session id: %w", err)
	}
	return id, nil
}

func (s *Store) ActorByTokenHash(ctx context.Context, tokenHash string, now time.Time) (domain.Actor, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT u.id, s.id, u.email, u.role, u.active, s.expires_at, s.revoked_at
        FROM sessions s JOIN users u ON u.id = s.user_id
        WHERE s.token_hash = ?`, tokenHash)
	var actor domain.Actor
	var role string
	var active int
	var expires string
	var revoked sql.NullString
	if err := row.Scan(&actor.UserID, &actor.SessionID, &actor.Email, &role, &active, &expires, &revoked); err != nil {
		return domain.Actor{}, normalizeError("authenticate session", err)
	}
	if active != 1 || revoked.Valid {
		return domain.Actor{}, fmt.Errorf("session unavailable: %w", apperr.ErrUnauthorized)
	}
	expiresAt, err := parseTime(expires)
	if err != nil {
		return domain.Actor{}, err
	}
	if !expiresAt.After(now) {
		return domain.Actor{}, fmt.Errorf("session expired at %s: %w", expiresAt, apperr.ErrExpired)
	}
	actor.Role = domain.Role(role)
	if _, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE id = ?", formatTime(now), actor.SessionID); err != nil {
		return domain.Actor{}, normalizeError("touch session", err)
	}
	return actor, nil
}

func (s *Store) RevokeSession(ctx context.Context, sessionID, userID int64, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
        UPDATE sessions SET revoked_at = ?
        WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, formatTime(now), sessionID, userID)
	if err != nil {
		return normalizeError("revoke session", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read revoked session count: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session already revoked: %w", apperr.ErrUnauthorized)
	}
	return nil
}

func (s *Store) RevokeAllUserSessions(ctx context.Context, userID int64, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
        UPDATE sessions SET revoked_at = ?
        WHERE user_id = ? AND revoked_at IS NULL`, formatTime(now), userID)
	if err != nil {
		return 0, normalizeError("revoke user sessions", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read revoked user session count: %w", err)
	}
	return count, nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	now = now.UTC()
	result, err := s.db.ExecContext(ctx, `
        DELETE FROM sessions
        WHERE expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at <= ?)`, formatTime(now), formatTime(now.Add(-24*time.Hour)))
	if err != nil {
		return 0, normalizeError("delete expired sessions", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted session count: %w", err)
	}
	return count, nil
}
