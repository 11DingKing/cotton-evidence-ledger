package storage

import (
	"context"
	"fmt"
	"time"
)

type Notification struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	DedupeKey string     `json:"dedupe_key"`
	Kind      string     `json:"kind"`
	Payload   string     `json:"payload"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s *Store) CreateNotification(ctx context.Context, userID int64, dedupeKey, kind, payload string, now time.Time) (Notification, bool, error) {
	result, err := s.db.ExecContext(ctx, `
        INSERT INTO notifications(user_id, dedupe_key, kind, payload, created_at)
        VALUES(?,?,?,?,?) ON CONFLICT(user_id,dedupe_key) DO NOTHING`, userID, dedupeKey, kind, payload, formatTime(now))
	if err != nil {
		return Notification{}, false, normalizeError("create notification", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Notification{}, false, fmt.Errorf("read notification insert count: %w", err)
	}
	if count == 0 {
		return Notification{}, false, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Notification{}, false, fmt.Errorf("read notification id: %w", err)
	}
	return Notification{ID: id, UserID: userID, DedupeKey: dedupeKey, Kind: kind,
		Payload: payload, CreatedAt: now.UTC()}, true, nil
}

func (s *Store) ListNotifications(ctx context.Context, userID, afterID int64, limit int) ([]Notification, int64, error) {
	if afterID < 0 { afterID = 0 }
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, user_id, dedupe_key, kind, payload, read_at, created_at
        FROM notifications WHERE user_id = ? AND id > ? ORDER BY id LIMIT ?`, userID, afterID, limit+1)
	if err != nil {
		return nil, 0, normalizeError("list notifications", err)
	}
	defer rows.Close()
	items := make([]Notification, 0, limit)
	var next int64
	for rows.Next() {
		var item Notification
		var readAt interface{ Scan(any) error }
		_ = readAt
		var readValue, created string
		var nullableRead *string
		if err := rows.Scan(&item.ID, &item.UserID, &item.DedupeKey, &item.Kind,
			&item.Payload, &nullableRead, &created); err != nil {
			return nil, 0, normalizeError("scan notification", err)
		}
		if nullableRead != nil {
			readValue = *nullableRead
			parsed, err := parseTime(readValue)
			if err != nil {
				return nil, 0, err
			}
			item.ReadAt = &parsed
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, 0, err
		}
		if len(items) == limit {
			next = items[0].ID
			break
		}
		items = append(items, item)
	}
	return items, next, rows.Err()
}
