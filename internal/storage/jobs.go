package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func (s *Store) EnqueueJob(ctx context.Context, job domain.Job, now time.Time) (domain.Job, error) {
	if job.ObjectID == "" { job.ObjectID = "0" }
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 5
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = now
	}
	result, err := s.db.ExecContext(ctx, `
        INSERT INTO jobs(kind, object_type, object_id, payload, status, attempts, max_attempts,
            available_at, lease_owner, last_error, created_at, updated_at)
        VALUES(?,?,?,?,'pending',0,?,?, '', '', ?,?)`, job.Kind, job.ObjectType, job.ObjectID,
		job.Payload, job.MaxAttempts, formatTime(job.AvailableAt), formatTime(now), formatTime(now))
	if err != nil {
		return domain.Job{}, normalizeError("enqueue job", err)
	}
	job.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Job{}, fmt.Errorf("read job id: %w", err)
	}
	job.Status = "pending"
	job.CreatedAt = now.UTC()
	job.UpdatedAt = now.UTC()
	return job, nil
}

func (s *Store) RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs SET status = 'retry', lease_owner = '', lease_until = NULL,
            available_at = ?, updated_at = ?, last_error = CASE
                WHEN last_error = '' THEN 'worker lease expired'
                ELSE last_error || '; worker lease expired' END
        WHERE status = 'running' AND lease_until <= ?`, formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return 0, normalizeError("recover expired job leases", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read recovered job count: %w", err)
	}
	return count, nil
}

func (s *Store) ClaimJob(ctx context.Context, workerID string, lease time.Duration, now time.Time) (domain.Job, error) {
	var claimed domain.Job
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRowContext(ctx, `
            SELECT id FROM jobs
            WHERE status IN ('pending','retry') AND available_at <= ?
            ORDER BY available_at, id LIMIT 1`, formatTime(now)).Scan(&id)
		if err != nil {
			return normalizeError("find claimable job", err)
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE jobs SET status = 'running', attempts = attempts + 1, lease_owner = ?,
                lease_until = ?, updated_at = ?
            WHERE id = ? AND status IN ('pending','retry') AND available_at <= ?`,
			workerID, formatTime(now.Add(lease)), formatTime(now), id, formatTime(now))
		if err != nil {
			return normalizeError("claim job", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrConflict
		}
		claimed, err = jobByID(ctx, tx, id)
		return err
	})
	return claimed, err
}

func jobByID(ctx context.Context, db DBTX, id int64) (domain.Job, error) {
	row := db.QueryRowContext(ctx, `
        SELECT id, kind, object_type, object_id, payload, status, attempts, max_attempts,
               available_at, lease_owner, lease_until, last_error, created_at, updated_at
        FROM jobs WHERE id = ?`, id)
	var job domain.Job
	var available, created, updated string
	var leaseUntil sql.NullString
	if err := row.Scan(&job.ID, &job.Kind, &job.ObjectType, &job.ObjectID, &job.Payload,
		&job.Status, &job.Attempts, &job.MaxAttempts, &available, &job.LeaseOwner,
		&leaseUntil, &job.LastError, &created, &updated); err != nil {
		return domain.Job{}, normalizeError("find job", err)
	}
	var err error
	job.AvailableAt, err = parseTime(available)
	if err != nil {
		return domain.Job{}, err
	}
	job.LeaseUntil, err = nullableTime(leaseUntil)
	if err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Job{}, err
	}
	job.UpdatedAt, err = parseTime(updated)
	return job, err
}

func (s *Store) CompleteJob(ctx context.Context, id int64, workerID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs SET status = 'completed', lease_owner = '', lease_until = NULL,
            last_error = '', updated_at = ?
        WHERE id = ? AND status = 'running' AND lease_owner = ?`, formatTime(now), id, workerID)
	if err != nil {
		return normalizeError("complete job", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read completed job count: %w", err)
	}
	if count != 1 {
		return apperr.ErrConflict
	}
	return nil
}

func (s *Store) FailJob(ctx context.Context, id int64, workerID, message string, now time.Time) (string, error) {
	var status string
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		job, err := jobByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if job.Status != "running" || job.LeaseOwner != workerID {
			return apperr.ErrConflict
		}
		status = "retry"
		availableAt := now.Add(backoff(job.Attempts))
		if job.Attempts >= job.MaxAttempts {
			status = "failed"
			availableAt = now
		}
		_, err = tx.ExecContext(ctx, `
            UPDATE jobs SET status = ?, lease_owner = '', lease_until = NULL,
                available_at = ?, last_error = ?, updated_at = ?
            WHERE id = ? AND status = 'running' AND lease_owner = ?`, status, formatTime(availableAt),
			message, formatTime(now), id, workerID)
		return normalizeError("fail job", err)
	})
	return status, err
}

func (s *Store) JobCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM jobs GROUP BY status")
	if err != nil {
		return nil, normalizeError("count jobs", err)
	}
	defer rows.Close()
	counts := map[string]int{"pending": 0, "running": 0, "retry": 0, "completed": 0, "failed": 0}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, normalizeError("scan job count", err)
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
