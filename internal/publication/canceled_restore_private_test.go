package publication_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/evidence"
	"github.com/11DingKing/cotton-evidence-ledger/internal/publication"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

func TestCanceledRestoreLeavesArchivedEvidenceUntouched(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "canceled-restore.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	author, err := store.CreateUser(context.Background(), "cancel-author@example.test", "Author", "hash", domain.RoleResearcher, now)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := store.CreateUser(context.Background(), "cancel-reviewer@example.test", "Reviewer", "hash", domain.RoleReviewer, now)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateUser(context.Background(), "cancel-owner@example.test", "Owner", "hash", domain.RoleKnowledgeOwner, now)
	if err != nil {
		t.Fatal(err)
	}
	authorActor := domain.Actor{UserID: author.ID, Email: author.Email, Role: author.Role}
	reviewerActor := domain.Actor{UserID: reviewer.ID, Email: reviewer.Email, Role: reviewer.Role}
	ownerActor := domain.Actor{UserID: owner.ID, Email: owner.Email, Role: owner.Role}
	clock := func() time.Time { return now }
	evidenceService := evidence.New(store).WithClock(clock)
	registered, err := evidenceService.Register(context.Background(), authorActor, evidence.RegisterInput{Kind: domain.SourcePaper, ExternalID: "cancel-paper-1", Title: "Canceled restore source", Origin: "cotton institute", Abstract: "A detailed abstract for the cancellation boundary."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evidenceService.AddClaim(context.Background(), authorActor, registered.Evidence.ID, registered.Version.ID, registered.Evidence.Revision, domain.Claim{Statement: "The sample is traceable", Locator: "section 1", Confidence: 0.91}); err != nil {
		t.Fatal(err)
	}
	evidenceRow, err := store.EvidenceByID(context.Background(), registered.Evidence.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviewService := reviews.New(store).WithClock(clock)
	if _, err := reviewService.Submit(context.Background(), authorActor, registered.Evidence.ID, registered.Version.ID, evidenceRow.Revision); err != nil {
		t.Fatal(err)
	}
	submitted, err := store.VersionByID(context.Background(), registered.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	slot, err := reviewService.Claim(context.Background(), reviewerActor, submitted.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reviewService.Decide(context.Background(), reviewerActor, slot.ID, domain.ReviewApprove, "The source and claim agree."); err != nil {
		t.Fatal(err)
	}
	evidenceRow, _ = store.EvidenceByID(context.Background(), registered.Evidence.ID)
	publicationService := publication.New(store).WithClock(clock)
	if _, err := publicationService.Publish(context.Background(), ownerActor, registered.Evidence.ID, registered.Version.ID, evidenceRow.Revision, nil); err != nil {
		t.Fatal(err)
	}
	evidenceRow, _ = store.EvidenceByID(context.Background(), registered.Evidence.ID)
	if err := publicationService.Withdraw(context.Background(), ownerActor, registered.Evidence.ID, evidenceRow.Revision, "The source supplied a formal withdrawal notice"); err != nil {
		t.Fatal(err)
	}
	evidenceRow, _ = store.EvidenceByID(context.Background(), registered.Evidence.ID)
	if err := publicationService.Archive(context.Background(), ownerActor, registered.Evidence.ID, evidenceRow.Revision, "Keep the withdrawn record for institutional traceability"); err != nil {
		t.Fatal(err)
	}
	archived, err := store.EvidenceByID(context.Background(), registered.Evidence.ID)
	if err != nil {
		t.Fatal(err)
	}
	countsBefore, err := store.JobCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = publicationService.Restore(canceled, ownerActor, archived.ID, archived.Revision, "The source corrected the record and requests a fresh review")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled restore error=%v, want context.Canceled", err)
	}
	after, err := store.EvidenceByID(context.Background(), archived.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != domain.EvidenceArchived || after.Revision != archived.Revision {
		t.Fatalf("canceled restore changed evidence: before=%#v after=%#v", archived, after)
	}
	version, err := store.VersionByID(context.Background(), registered.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if version.State != domain.VersionPublished {
		t.Fatalf("canceled restore changed version state=%s", version.State)
	}
	countsAfter, err := store.JobCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countsAfter["pending"] != countsBefore["pending"] {
		t.Fatalf("canceled restore queued work: before=%#v after=%#v", countsBefore, countsAfter)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
