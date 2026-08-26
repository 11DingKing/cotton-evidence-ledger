package jobs_test

import (
	"context"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/jobs"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func TestEnqueueUsesRetrySafeAttemptDefault(t *testing.T) {
	ctx := context.Background()
	st, e := storage.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	now := time.Now().UTC()
	u, e := st.CreateUser(ctx, "owner4@example.test", "Owner Four", "hash", domain.RoleKnowledgeOwner, now)
	if e != nil {
		t.Fatal(e)
	}
	a := domain.Actor{UserID: u.ID, Role: u.Role}
	j, e := jobs.New(st).Enqueue(ctx, a, jobs.KindIntegrityCheck, "evidence", 1, map[string]any{"check": true})
	if e != nil {
		t.Fatal(e)
	}
	if j.MaxAttempts != 5 {
		t.Fatalf("max attempts=%d want 5", j.MaxAttempts)
	}
}
