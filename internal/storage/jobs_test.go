package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func TestJobClaimCompleteAndCounts(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	job, err := store.EnqueueJob(ctx, domain.Job{Kind: "integrity_check", ObjectType: "audit_chain",
		ObjectID: "global", Payload: `{}`, MaxAttempts: 3}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "pending" || job.Attempts != 0 {
		t.Fatalf("enqueued job=%#v", job)
	}
	claimed, err := store.ClaimJob(ctx, "worker-a", 30*time.Second, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID || claimed.Status != "running" || claimed.Attempts != 1 || claimed.LeaseOwner != "worker-a" {
		t.Fatalf("claimed job=%#v", claimed)
	}
	if claimed.LeaseUntil == nil || !claimed.LeaseUntil.Equal(fixedNow.Add(30*time.Second)) {
		t.Fatalf("lease until=%v", claimed.LeaseUntil)
	}
	if err := store.CompleteJob(ctx, job.ID, "worker-b", fixedNow.Add(time.Second)); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("wrong worker completed job: %v", err)
	}
	if err := store.CompleteJob(ctx, job.ID, "worker-a", fixedNow.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	counts, err := store.JobCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["completed"] != 1 || counts["running"] != 0 || counts["pending"] != 0 {
		t.Fatalf("job counts=%#v", counts)
	}
	if _, err := store.ClaimJob(ctx, "worker-a", time.Minute, fixedNow.Add(time.Hour)); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("empty queue error=%v", err)
	}
}

func TestJobFailureRetriesThenBecomesPermanent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	job, err := store.EnqueueJob(ctx, domain.Job{Kind: "import_resume", ObjectType: "import",
		ObjectID: "batch-1", Payload: `{}`, MaxAttempts: 2}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimJob(ctx, "worker-a", time.Minute, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	status, err := store.FailJob(ctx, first.ID, "worker-a", "temporary parse failure", fixedNow.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status != "retry" {
		t.Fatalf("first failure status=%s", status)
	}
	if _, err := store.ClaimJob(ctx, "worker-a", time.Minute, fixedNow.Add(1500*time.Millisecond)); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("job ignored backoff: %v", err)
	}
	second, err := store.ClaimJob(ctx, "worker-a", time.Minute, fixedNow.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != job.ID || second.Attempts != 2 {
		t.Fatalf("second attempt=%#v", second)
	}
	status, err = store.FailJob(ctx, second.ID, "worker-a", "permanent validation failure", fixedNow.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("final status=%s", status)
	}
	counts, _ := store.JobCounts(ctx)
	if counts["failed"] != 1 || counts["retry"] != 0 {
		t.Fatalf("counts=%#v", counts)
	}
}

func TestExpiredWorkerLeaseIsRecoveredAfterRestartBoundary(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	job, err := store.EnqueueJob(ctx, domain.Job{Kind: "expiry_reminder", ObjectType: "evidence",
		ObjectID: "22", Payload: `{"user_id":1}`, MaxAttempts: 4}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimJob(ctx, "dead-worker", 10*time.Second, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed ID=%d, want %d", claimed.ID, job.ID)
	}
	if recovered, err := store.RecoverExpiredLeases(ctx, fixedNow.Add(9*time.Second)); err != nil || recovered != 0 {
		t.Fatalf("early recovery=%d err=%v", recovered, err)
	}
	if recovered, err := store.RecoverExpiredLeases(ctx, fixedNow.Add(11*time.Second)); err != nil || recovered != 1 {
		t.Fatalf("expired recovery=%d err=%v", recovered, err)
	}
	reclaimed, err := store.ClaimJob(ctx, "replacement-worker", time.Minute, fixedNow.Add(11*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Attempts != 2 || reclaimed.LeaseOwner != "replacement-worker" || reclaimed.LastError == "" {
		t.Fatalf("reclaimed job=%#v", reclaimed)
	}
}

func TestJobOperationsHonorCanceledContext(t *testing.T) {
	store := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.EnqueueJob(ctx, domain.Job{Kind: "integrity_check", ObjectType: "audit", ObjectID: "all", Payload: `{}`}, fixedNow)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("enqueue with canceled context=%v", err)
	}
	_, err = store.ClaimJob(ctx, "worker", time.Minute, fixedNow)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("claim with canceled context=%v", err)
	}
}

func TestIdempotencyReservationReplayAndPayloadConflict(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	err := store.InTx(ctx, func(tx *sql.Tx) error {
		record, inserted, err := ReserveIdempotency(ctx, tx, "POST /v1/evidence", "request-key", "hash-a", fixedNow)
		if err != nil {
			return err
		}
		if !inserted || record.Committed {
			t.Fatalf("initial reservation=%#v inserted=%v", record, inserted)
		}
		return CommitIdempotency(ctx, tx, record.Scope, record.Key, 201, `{"evidence_id":10}`, fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.InTx(ctx, func(tx *sql.Tx) error {
		record, inserted, err := ReserveIdempotency(ctx, tx, "POST /v1/evidence", "request-key", "hash-a", fixedNow.Add(time.Minute))
		if err != nil {
			return err
		}
		if inserted || !record.Committed || record.ResponseCode != 201 || record.ResponseBody != `{"evidence_id":10}` {
			t.Fatalf("replay record=%#v inserted=%v", record, inserted)
		}
		_, _, err = ReserveIdempotency(ctx, tx, "POST /v1/evidence", "request-key", "hash-b", fixedNow.Add(time.Minute))
		if !errors.Is(err, apperr.ErrConflict) {
			t.Fatalf("changed payload error=%v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExpiredIdempotencyKeysAreDeleted(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `
        INSERT INTO idempotency_keys(scope,key,request_hash,expires_at,created_at)
        VALUES('a','expired','h',?,?),('a','active','h',?,?)`,
		formatTime(fixedNow.Add(-time.Second)), formatTime(fixedNow.Add(-time.Hour)),
		formatTime(fixedNow.Add(time.Hour)), formatTime(fixedNow)); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteExpiredIdempotency(ctx, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d", deleted)
	}
	var key string
	if err := store.db.QueryRowContext(ctx, "SELECT key FROM idempotency_keys").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != "active" {
		t.Fatalf("remaining key=%q", key)
	}
}

func TestNotificationsAreDeduplicatedAndPaginated(t *testing.T) {
	store := openTestStore(t)
	user := createUser(t, store, "notify@example.test", domain.RoleReviewer)
	ctx := context.Background()
	first, created, err := store.CreateNotification(ctx, user.ID, "due-1", "overdue_review", `{"slot":1}`, fixedNow)
	if err != nil || !created {
		t.Fatalf("create first notification: %#v %v %v", first, created, err)
	}
	duplicate, created, err := store.CreateNotification(ctx, user.ID, "due-1", "overdue_review", `{"slot":1}`, fixedNow)
	if err != nil || created || duplicate.ID != 0 {
		t.Fatalf("duplicate notification=%#v created=%v err=%v", duplicate, created, err)
	}
	second, created, err := store.CreateNotification(ctx, user.ID, "due-2", "overdue_review", `{"slot":2}`, fixedNow.Add(time.Second))
	if err != nil || !created {
		t.Fatal(err)
	}
	items, next, err := store.ListNotifications(ctx, user.ID, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != first.ID || next != first.ID {
		t.Fatalf("first notification page=%#v next=%d", items, next)
	}
	items, next, err = store.ListNotifications(ctx, user.ID, next, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != second.ID || next != 0 {
		t.Fatalf("second notification page=%#v next=%d", items, next)
	}
}
