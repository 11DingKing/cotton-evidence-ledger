package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
)

type IdempotencyRecord struct {
	Scope        string
	Key          string
	RequestHash  string
	ResponseCode int
	ResponseBody string
	Committed    bool
}

func ReserveIdempotency(ctx context.Context, tx *sql.Tx, scope, key, requestHash string, now time.Time) (IdempotencyRecord, bool, error) {
	result, err := tx.ExecContext(ctx, `
        INSERT INTO idempotency_keys(scope, key, request_hash, expires_at, created_at)
        VALUES(?,?,?,?,?) ON CONFLICT(scope,key) DO NOTHING`, scope, key, requestHash,
		formatTime(now.Add(24*time.Hour)), formatTime(now))
	if err != nil {
		return IdempotencyRecord{}, false, normalizeError("reserve idempotency key", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return IdempotencyRecord{}, false, fmt.Errorf("read idempotency reservation count: %w", err)
	}
	if inserted == 1 {
		return IdempotencyRecord{Scope: scope, Key: key, RequestHash: requestHash}, true, nil
	}
	var record IdempotencyRecord
	var code sql.NullInt64
	var body sql.NullString
	err = tx.QueryRowContext(ctx, `
        SELECT scope, key, request_hash, response_code, response_body, committed_at IS NOT NULL
        FROM idempotency_keys WHERE scope = ? AND key = ?`, scope, key).Scan(
		&record.Scope, &record.Key, &record.RequestHash, &code, &body, &record.Committed)
	if err != nil {
		return IdempotencyRecord{}, false, normalizeError("read idempotency key", err)
	}
	if record.RequestHash != requestHash {
		return IdempotencyRecord{}, false, fmt.Errorf("idempotency key payload changed: %w", apperr.ErrConflict)
	}
	if code.Valid {
		record.ResponseCode = int(code.Int64)
	}
	if body.Valid {
		record.ResponseBody = body.String
	}
	return record, false, nil
}

func CommitIdempotency(ctx context.Context, tx *sql.Tx, scope, key string, responseCode int, responseBody string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
        UPDATE idempotency_keys
        SET response_code = ?, response_body = ?, committed_at = ?
        WHERE scope = ? AND key = ? AND committed_at IS NULL`, responseCode, responseBody, formatTime(now), scope, key)
	if err != nil {
		return normalizeError("commit idempotency response", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read idempotency commit count: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("idempotency response already committed: %w", apperr.ErrConflict)
	}
	return nil
}

func (s *Store) DeleteExpiredIdempotency(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM idempotency_keys WHERE expires_at <= ?", formatTime(now))
	if err != nil {
		return 0, normalizeError("delete expired idempotency keys", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read expired idempotency count: %w", err)
	}
	return count, nil
}
