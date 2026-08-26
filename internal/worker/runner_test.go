package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/jobs"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type recordingHandler struct {
	mu      sync.Mutex
	jobs    []domain.Job
	err     error
	started chan struct{}
}

func (h *recordingHandler) Handle(ctx context.Context, job domain.Job) error {
	if h.started != nil {
		select {
		case h.started <- struct{}{}:
		default:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	h.jobs = append(h.jobs, job)
	h.mu.Unlock()
	return h.err
}

func (h *recordingHandler) handled() []domain.Job {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]domain.Job(nil), h.jobs...)
}

func TestRunnerProcessesAndCompletesOneJob(t *testing.T) {
	store := openWorkerStore(t)
	now := time.Now().UTC()
	job, err := store.EnqueueJob(context.Background(), domain.Job{Kind: jobs.KindIntegrityCheck,
		ObjectType: "audit_chain", ObjectID: "global", Payload: `{}`, MaxAttempts: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	handler := &recordingHandler{}
	runner := New(store, handler, testLogger(), "worker-success", time.Second, time.Minute).
		WithClock(func() time.Time { return now })
	if err := runner.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	handled := handler.handled()
	if len(handled) != 1 || handled[0].ID != job.ID || handled[0].Attempts != 1 {
		t.Fatalf("handled jobs=%#v", handled)
	}
	counts, err := store.JobCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts["completed"] != 1 || counts["running"] != 0 {
		t.Fatalf("counts=%#v", counts)
	}
}

func TestRunnerRecordsRetryWithoutReturningHandlerFailure(t *testing.T) {
	store := openWorkerStore(t)
	now := time.Now().UTC()
	job, err := store.EnqueueJob(context.Background(), domain.Job{Kind: jobs.KindImportResume,
		ObjectType: "import", ObjectID: "batch", Payload: `{}`, MaxAttempts: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	handler := &recordingHandler{err: errors.New("temporary downstream failure")}
	runner := New(store, handler, testLogger(), "worker-retry", time.Second, time.Minute).
		WithClock(func() time.Time { return now })
	if err := runner.processOne(context.Background()); err != nil {
		t.Fatalf("handler failure should be persisted, got %v", err)
	}
	counts, err := store.JobCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts["retry"] != 1 || counts["completed"] != 0 {
		t.Fatalf("counts=%#v", counts)
	}
	if len(handler.handled()) != 1 || handler.handled()[0].ID != job.ID {
		t.Fatalf("handler calls=%#v", handler.handled())
	}
}

func TestRunnerHonorsCanceledContext(t *testing.T) {
	store := openWorkerStore(t)
	now := time.Now().UTC()
	_, err := store.EnqueueJob(context.Background(), domain.Job{Kind: jobs.KindIntegrityCheck,
		ObjectType: "audit", ObjectID: "global", Payload: `{}`, MaxAttempts: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := New(store, &recordingHandler{}, testLogger(), "worker-canceled", time.Second, time.Minute).
		WithClock(func() time.Time { return now })
	if err := runner.processOne(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("processOne canceled error=%v", err)
	}
	counts, err := store.JobCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts["pending"] != 1 || counts["running"] != 0 {
		t.Fatalf("canceled claim changed job: %#v", counts)
	}
}

func TestRunnerRunStopsGracefully(t *testing.T) {
	store := openWorkerStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	runner := New(store, &recordingHandler{}, testLogger(), "worker-loop", time.Millisecond, time.Second)
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	runner.Wait()
}

func TestDispatcherValidatesIntegrityAndImportPayload(t *testing.T) {
	store := openWorkerStore(t)
	dispatcher := NewDispatcher(store, reviews.New(store))
	ctx := context.Background()
	if err := dispatcher.Handle(ctx, domain.Job{Kind: jobs.KindIntegrityCheck, ObjectType: "audit",
		ObjectID: "global", Payload: `{}`}); err != nil {
		t.Fatalf("empty audit chain integrity: %v", err)
	}
	validImport := domain.Job{Kind: jobs.KindImportResume, ObjectType: "import", ObjectID: "one",
		Payload: `{"completed":["source-a"],"pending":["source-a","source-b"]}`}
	if err := dispatcher.Handle(ctx, validImport); err != nil {
		t.Fatalf("valid resumable import: %v", err)
	}
	tests := []domain.Job{
		{Kind: "unknown", Payload: `{}`},
		{Kind: jobs.KindImportResume, Payload: `{bad`},
		{Kind: jobs.KindImportResume, Payload: `{"pending":[""]}`},
		{Kind: jobs.KindExpiryReminder, Payload: `{}`},
	}
	for _, job := range tests {
		if err := dispatcher.Handle(ctx, job); err == nil {
			t.Errorf("invalid job %#v unexpectedly succeeded", job)
		}
	}
}

func TestDispatcherHonorsCancellationBeforeWork(t *testing.T) {
	store := openWorkerStore(t)
	dispatcher := NewDispatcher(store, reviews.New(store))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := dispatcher.Handle(ctx, domain.Job{Kind: jobs.KindIntegrityCheck, Payload: `{}`})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dispatcher error=%v", err)
	}
}

func openWorkerStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
