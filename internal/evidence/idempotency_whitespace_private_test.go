package evidence_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/evidence"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

func TestIdempotencyKeyWhitespaceReplaysCommittedRegistration(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "idempotency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC)
	user, err := store.CreateUser(ctx, "idem-author@example.test", "Idempotency Author", "hash", domain.RoleResearcher, now)
	if err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{UserID: user.ID, Email: user.Email, Role: user.Role}
	svc := evidence.New(store).WithClock(func() time.Time { return now })
	input := evidence.RegisterInput{Kind: domain.SourcePaper, ExternalID: "idem-paper", Title: "Whitespace key source", Origin: "cotton institute", Abstract: "A stable abstract for an idempotent registration."}
	first, err := svc.RegisterIdempotent(ctx, actor, input, "  batch-key  ")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.RegisterIdempotent(ctx, actor, input, "batch-key")
	if err != nil {
		t.Fatalf("normalized key should replay: %v", err)
	}
	if replay.Evidence.ID != first.Evidence.ID || replay.Version.ID != first.Version.ID {
		t.Fatalf("whitespace variant created a new registration: first=%#v replay=%#v", first, replay)
	}
}
