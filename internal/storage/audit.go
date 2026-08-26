package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func AppendAudit(ctx context.Context, tx DBTX, payload audit.Payload) (domain.AuditEvent, error) {
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT event_hash FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return domain.AuditEvent{}, normalizeError("read previous audit hash", err)
	}
	if err == sql.ErrNoRows {
		previous = audit.GenesisHash
	}
	event, err := audit.Build(previous, payload)
	if err != nil {
		return domain.AuditEvent{}, err
	}
	result, err := tx.ExecContext(ctx, `
        INSERT INTO audit_events(actor_id, action, object_type, object_id, result,
            request_id, before_json, after_json, previous_hash, event_hash, created_at)
        VALUES(?,?,?,?,?,?,?,?,?,?,?)`, event.ActorID, event.Action, event.ObjectType,
		event.ObjectID, event.Result, event.RequestID, event.BeforeJSON, event.AfterJSON,
		event.PreviousHash, event.EventHash, formatTime(event.CreatedAt))
	if err != nil {
		return domain.AuditEvent{}, normalizeError("append audit event", err)
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("read audit event id: %w", err)
	}
	return event, nil
}

func (s *Store) AuditEvents(ctx context.Context, afterID int64, limit int) (domain.Page[domain.AuditEvent], error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, actor_id, action, object_type, object_id, result, request_id,
               before_json, after_json, previous_hash, event_hash, created_at
        FROM audit_events WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit+1)
	if err != nil {
		return domain.Page[domain.AuditEvent]{}, normalizeError("list audit events", err)
	}
	defer rows.Close()
	page := domain.Page[domain.AuditEvent]{Items: make([]domain.AuditEvent, 0, limit)}
	for rows.Next() {
		var event domain.AuditEvent
		var actorID sql.NullInt64
		var created string
		if err := rows.Scan(&event.ID, &actorID, &event.Action, &event.ObjectType, &event.ObjectID,
			&event.Result, &event.RequestID, &event.BeforeJSON, &event.AfterJSON,
			&event.PreviousHash, &event.EventHash, &created); err != nil {
			return domain.Page[domain.AuditEvent]{}, normalizeError("scan audit event", err)
		}
		if actorID.Valid {
			event.ActorID = &actorID.Int64
		}
		event.CreatedAt, err = parseTime(created)
		if err != nil {
			return domain.Page[domain.AuditEvent]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = page.Items[len(page.Items)-1].ID
			break
		}
		page.Items = append(page.Items, event)
	}
	if err := rows.Err(); err != nil {
		return domain.Page[domain.AuditEvent]{}, fmt.Errorf("iterate audit events: %w", err)
	}
	return page, nil
}

func (s *Store) VerifyAuditChain(ctx context.Context) error {
	page, err := s.AuditEvents(ctx, 0, 200)
	if err != nil {
		return err
	}
	events := append([]domain.AuditEvent(nil), page.Items...)
	for page.NextCursor != 0 {
		page, err = s.AuditEvents(ctx, page.NextCursor, 200)
		if err != nil {
			return err
		}
		events = append(events, page.Items...)
	}
	if err := audit.Verify(events); err != nil {
		return fmt.Errorf("verify audit chain at %s: %w", time.Now().UTC().Format(time.RFC3339), err)
	}
	return nil
}
