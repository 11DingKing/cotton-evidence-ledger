package publication_test

import (
	"context"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/publication"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func TestExpiredHandoffCannotTransferOwnership(t *testing.T) {
	ctx := context.Background()
	st, e := storage.Open(ctx, filepath.Join(t.TempDir(), "h.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	now := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	from, e := st.CreateUser(ctx, "from5@example.test", "From Five", "hash", domain.RoleKnowledgeOwner, now)
	if e != nil {
		t.Fatal(e)
	}
	to, e := st.CreateUser(ctx, "to5@example.test", "To Five", "hash", domain.RoleResearcher, now)
	if e != nil {
		t.Fatal(e)
	}
	ev, _, e := st.RegisterEvidence(ctx, storage.RegisterEvidenceParams{Source: domain.Source{Kind: domain.SourcePaper, ExternalID: "h5", Title: "Handoff", Origin: "institute", Fingerprint: "h5"}, Version: domain.EvidenceVersion{State: domain.VersionDraft, Title: "Handoff", Abstract: "A sufficiently detailed abstract for handoff expiry.", ContentHash: "h5", CreatedBy: from.ID}, OwnerID: from.ID, Now: now})
	if e != nil {
		t.Fatal(e)
	}
	h, e := publication.New(st).WithClock(func() time.Time { return now }).CreateHandoff(ctx, domain.Actor{UserID: from.ID, Role: from.Role}, ev.ID, to.ID, ev.Revision, "Transfer responsibility", now.Add(-30*time.Second))
	if e == nil {
		t.Fatal("past handoff unexpectedly accepted")
	}
	_ = h
}
