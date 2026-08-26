package worker_test

import (
	"context"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
	"github.com/11DingKing/cotton-evidence-ledger/internal/worker"
	"path/filepath"
	"testing"
	"time"
)

func TestEmptyReminderMessageIsRejected(t *testing.T) {
	st, e := storage.Open(context.Background(), filepath.Join(t.TempDir(), "w.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	if _, e = st.CreateUser(context.Background(), "worker7@example.test", "Worker Seven", "hash", domain.RoleKnowledgeOwner, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	d := worker.NewDispatcher(st, reviews.New(st))
	e = d.Handle(context.Background(), domain.Job{ID: 7, Kind: "expiry_reminder", Payload: `{"user_id":1,"message":""}`})
	if e == nil {
		t.Fatal("empty reminder message was accepted")
	}
}
