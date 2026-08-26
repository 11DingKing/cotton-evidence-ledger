package identity_test

import (
	"context"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/identity"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupRemovesSessionAtExactExpiry(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	user, err := store.CreateUser(ctx, "expiry@example.test", "Expiry User", "hash", domain.RoleResearcher, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, user.ID, "exact-expiry", now, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	svc := identity.New(store, time.Hour).WithClock(func() time.Time { return now })
	removed, err := svc.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("cleanup removed=%d, want 1 at exact expiry", removed)
	}
	if _, err := store.ActorByTokenHash(ctx, "exact-expiry", now); err == nil {
		t.Fatal("expired session remained usable")
	}
}
