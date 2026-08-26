package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func TestReviewLifecycleEnforcesSubmitterIsolationAndExclusiveSlot(t *testing.T) {
	store := openTestStore(t)
	researcher := createUser(t, store, "review-author@example.test", domain.RoleResearcher)
	reviewer := createUser(t, store, "reviewer@example.test", domain.RoleReviewer)
	otherReviewer := createUser(t, store, "reviewer-two@example.test", domain.RoleReviewer)
	evidence, version := registerTestEvidence(t, store, researcher, "review-lifecycle")
	claim := domain.Claim{Statement: "The measured cotton fiber strength exceeds the control", Locator: "section 3.2", Confidence: 0.93}
	if _, err := store.AddClaim(context.Background(), actorFor(researcher), evidence.ID, version.ID, claim,
		evidence.Revision, "claim", fixedNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	updatedEvidence, _ := store.EvidenceByID(context.Background(), evidence.ID)
	submitted, err := store.SubmitForReview(context.Background(), actorFor(researcher), evidence.ID, version.ID,
		updatedEvidence.Revision, "submit", fixedNow.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if submitted.State != domain.VersionUnderReview {
		t.Fatalf("submitted state=%s", submitted.State)
	}
	if _, err := store.ClaimReviewSlot(context.Background(), actorFor(researcher), version.ID,
		fixedNow.Add(24*time.Hour), "self-review", fixedNow.Add(3*time.Minute)); !errors.Is(err, apperr.ErrSelfReview) {
		t.Fatalf("submitter claimed own slot: %v", err)
	}
	slot, err := store.ClaimReviewSlot(context.Background(), actorFor(reviewer), version.ID,
		fixedNow.Add(24*time.Hour), "claim-slot", fixedNow.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("reviewer claim: %v", err)
	}
	if _, err := store.ClaimReviewSlot(context.Background(), actorFor(otherReviewer), version.ID,
		fixedNow.Add(24*time.Hour), "second-slot", fixedNow.Add(5*time.Minute)); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("second reviewer should conflict, got %v", err)
	}
	review, err := store.DecideReview(context.Background(), actorFor(reviewer), slot.ID, domain.ReviewApprove,
		"The claims are supported by the cited measurements.", "decide", fixedNow.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("decide review: %v", err)
	}
	if review.Decision != domain.ReviewApprove || review.ReviewerID != reviewer.ID {
		t.Fatalf("review=%#v", review)
	}
	approved, _ := store.VersionByID(context.Background(), version.ID)
	if approved.State != domain.VersionApproved {
		t.Fatalf("approved version state=%s", approved.State)
	}
	finalEvidence, _ := store.EvidenceByID(context.Background(), evidence.ID)
	if finalEvidence.State != domain.EvidenceReviewing || finalEvidence.Revision != 4 {
		t.Fatalf("evidence after approval=%#v", finalEvidence)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("review audit chain invalid: %v", err)
	}
}

func TestConcurrentReviewSlotClaimsHaveExactlyOneWinner(t *testing.T) {
	store := openTestStore(t)
	author := createUser(t, store, "concurrent-author@example.test", domain.RoleResearcher)
	firstReviewer := createUser(t, store, "concurrent-one@example.test", domain.RoleReviewer)
	secondReviewer := createUser(t, store, "concurrent-two@example.test", domain.RoleReviewer)
	_, version := prepareSubmittedVersion(t, store, author, "concurrent-review-slot")
	start := make(chan struct{})
	type result struct {
		slot domain.ReviewSlot
		err  error
	}
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	claim := func(user domain.User) {
		ready.Done()
		<-start
		slot, err := store.ClaimReviewSlot(context.Background(), actorFor(user), version.ID,
			fixedNow.Add(24*time.Hour), "concurrent-slot", fixedNow.Add(5*time.Minute))
		results <- result{slot: slot, err: err}
	}
	go claim(firstReviewer)
	go claim(secondReviewer)
	ready.Wait()
	close(start)
	first := <-results
	second := <-results
	winners := 0
	conflicts := 0
	for _, outcome := range []result{first, second} {
		if outcome.err == nil {
			winners++
			if outcome.slot.ID == 0 || outcome.slot.VersionID != version.ID {
				t.Fatalf("winner returned invalid slot: %#v", outcome.slot)
			}
			continue
		}
		if errors.Is(outcome.err, apperr.ErrConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent claim error: %v", outcome.err)
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d outcomes=%#v %#v", winners, conflicts, first, second)
	}
	var active int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM review_slots WHERE version_id = ? AND status = 'claimed'`, version.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active claimed slots=%d, want 1", active)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("concurrent claim audit chain invalid: %v", err)
	}
}

func TestRequestChangesReturnsVersionToDraftAndReleasesSlot(t *testing.T) {
	store := openTestStore(t)
	author := createUser(t, store, "changes-author@example.test", domain.RoleResearcher)
	reviewer := createUser(t, store, "changes-reviewer@example.test", domain.RoleReviewer)
	evidence, version := prepareSubmittedVersion(t, store, author, "request-changes")
	slot, err := store.ClaimReviewSlot(context.Background(), actorFor(reviewer), version.ID,
		fixedNow.Add(time.Hour), "claim", fixedNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DecideReview(context.Background(), actorFor(reviewer), slot.ID, domain.ReviewRequestChanges,
		"Please reconcile the sample size with the source table.", "changes", fixedNow.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := store.VersionByID(context.Background(), version.ID)
	if updated.State != domain.VersionDraft {
		t.Fatalf("version state=%s", updated.State)
	}
	evidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	if evidence.State != domain.EvidenceExtracting {
		t.Fatalf("evidence state=%s", evidence.State)
	}
	var status string
	if err := store.db.QueryRow("SELECT status FROM review_slots WHERE id = ?", slot.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("slot status=%s", status)
	}
}

func TestExpiredReviewCannotDecideAndWorkerReleasesIt(t *testing.T) {
	store := openTestStore(t)
	author := createUser(t, store, "expired-author@example.test", domain.RoleResearcher)
	reviewer := createUser(t, store, "expired-reviewer@example.test", domain.RoleReviewer)
	_, version := prepareSubmittedVersion(t, store, author, "expired-review")
	due := fixedNow.Add(10 * time.Minute)
	slot, err := store.ClaimReviewSlot(context.Background(), actorFor(reviewer), version.ID, due,
		"claim", fixedNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DecideReview(context.Background(), actorFor(reviewer), slot.ID, domain.ReviewApprove,
		"The evidence is acceptable after cross-checking.", "late", due.Add(time.Second))
	if !errors.Is(err, apperr.ErrExpired) {
		t.Fatalf("late decision error=%v", err)
	}
	expired, err := store.ExpireReviewSlots(context.Background(), due.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0] != slot.ID {
		t.Fatalf("expired slots=%v", expired)
	}
	var reviews int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM reviews WHERE slot_id = ?", slot.ID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if reviews != 0 {
		t.Fatalf("expired decision left %d review rows", reviews)
	}
}

func TestPublishAndCorrectionReplacementAreConsistentTransactions(t *testing.T) {
	store := openTestStore(t)
	author := createUser(t, store, "publish-author@example.test", domain.RoleResearcher)
	reviewer := createUser(t, store, "publish-reviewer@example.test", domain.RoleReviewer)
	owner := createUser(t, store, "publish-owner@example.test", domain.RoleKnowledgeOwner)
	evidence, version := prepareApprovedVersion(t, store, author, reviewer, "publish-correct")
	currentEvidence, _ := store.EvidenceByID(context.Background(), evidence.ID)
	published, err := store.Publish(context.Background(), PublishParams{Actor: actorFor(owner), EvidenceID: evidence.ID,
		VersionID: version.ID, ExpectedRevision: currentEvidence.Revision, RequestID: "publish", Now: fixedNow.Add(10 * time.Minute)})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.State != domain.VersionPublished || published.PublishedAt == nil {
		t.Fatalf("published version=%#v", published)
	}
	currentEvidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	correction, err := store.StartCorrection(context.Background(), CorrectionParams{Actor: actorFor(author),
		EvidenceID: evidence.ID, PublishedVersionID: version.ID, ExpectedRevision: currentEvidence.Revision,
		Title: "Corrected cotton fiber evidence", Abstract: "A corrected abstract with enough detail for a second independent review.",
		ContentHash: "corrected-content", RequestID: "correction", Now: fixedNow.Add(11 * time.Minute)})
	if err != nil {
		t.Fatalf("start correction: %v", err)
	}
	currentEvidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	claim := domain.Claim{Statement: "The corrected sample excludes the contaminated batch", Locator: "appendix B", Confidence: 0.98}
	if _, err := store.AddClaim(context.Background(), actorFor(author), evidence.ID, correction.ID, claim,
		currentEvidence.Revision, "corrected-claim", fixedNow.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	currentEvidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	if _, err := store.SubmitForReview(context.Background(), actorFor(author), evidence.ID, correction.ID,
		currentEvidence.Revision, "correction-review", fixedNow.Add(13*time.Minute)); err != nil {
		t.Fatal(err)
	}
	slot, err := store.ClaimReviewSlot(context.Background(), actorFor(reviewer), correction.ID,
		fixedNow.Add(48*time.Hour), "correction-slot", fixedNow.Add(14*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideReview(context.Background(), actorFor(reviewer), slot.ID, domain.ReviewApprove,
		"The correction is consistent with the source appendix.", "correction-approve", fixedNow.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	currentEvidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	replacement, err := store.ReplacePublishedVersion(context.Background(), actorFor(owner), evidence.ID,
		version.ID, correction.ID, currentEvidence.Revision, "replace", fixedNow.Add(16*time.Minute))
	if err != nil {
		t.Fatalf("replace version: %v", err)
	}
	if replacement.State != domain.VersionPublished {
		t.Fatalf("replacement state=%s", replacement.State)
	}
	old, _ := store.VersionByID(context.Background(), version.ID)
	if old.State != domain.VersionSuperseded {
		t.Fatalf("old version state=%s", old.State)
	}
	currentEvidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	if currentEvidence.CurrentVersionID == nil || *currentEvidence.CurrentVersionID != correction.ID || currentEvidence.State != domain.EvidencePublished {
		t.Fatalf("evidence did not activate replacement: %#v", currentEvidence)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("correction audit chain invalid: %v", err)
	}
}

func TestPublishRollsBackWhenCitationTargetIsUnpublished(t *testing.T) {
	store := openTestStore(t)
	author := createUser(t, store, "rollback-author@example.test", domain.RoleResearcher)
	reviewer := createUser(t, store, "rollback-reviewer@example.test", domain.RoleReviewer)
	owner := createUser(t, store, "rollback-owner@example.test", domain.RoleKnowledgeOwner)
	evidence, version := prepareApprovedVersion(t, store, author, reviewer, "rollback-publication")
	_, unpublished := registerTestEvidence(t, store, author, "unpublished-target")
	current, _ := store.EvidenceByID(context.Background(), evidence.ID)
	_, err := store.Publish(context.Background(), PublishParams{Actor: actorFor(owner), EvidenceID: evidence.ID,
		VersionID: version.ID, ExpectedRevision: current.Revision, CitationTargets: []int64{unpublished.ID},
		RequestID: "invalid-publication", Now: fixedNow.Add(20 * time.Minute)})
	if !errors.Is(err, apperr.ErrInvalidState) {
		t.Fatalf("publish error=%v", err)
	}
	unchangedVersion, _ := store.VersionByID(context.Background(), version.ID)
	unchangedEvidence, _ := store.EvidenceByID(context.Background(), evidence.ID)
	if unchangedVersion.State != domain.VersionApproved || unchangedEvidence.State != domain.EvidenceReviewing {
		t.Fatalf("failed publication leaked state: %#v %#v", unchangedVersion, unchangedEvidence)
	}
	if countRows(t, store.db, "citations") != 0 {
		t.Fatal("failed publication leaked citation")
	}
}

func TestWithdrawArchiveAndRestoreQueueRevalidationAtomically(t *testing.T) {
	store := openTestStore(t)
	author := createUser(t, store, "restore-author@example.test", domain.RoleResearcher)
	reviewer := createUser(t, store, "restore-reviewer@example.test", domain.RoleReviewer)
	owner := createUser(t, store, "restore-owner@example.test", domain.RoleKnowledgeOwner)
	evidence, version := prepareApprovedVersion(t, store, author, reviewer, "restore-flow")
	current, _ := store.EvidenceByID(context.Background(), evidence.ID)
	if _, err := store.Publish(context.Background(), PublishParams{Actor: actorFor(owner), EvidenceID: evidence.ID,
		VersionID: version.ID, ExpectedRevision: current.Revision, RequestID: "publish", Now: fixedNow.Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	current, _ = store.EvidenceByID(context.Background(), evidence.ID)
	if err := store.WithdrawEvidence(context.Background(), actorFor(owner), evidence.ID, current.Revision,
		"Source institution retracted the underlying table", "withdraw", fixedNow.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, _ = store.EvidenceByID(context.Background(), evidence.ID)
	if current.State != domain.EvidenceWithdrawn {
		t.Fatalf("withdrawn state=%s", current.State)
	}
	if err := store.ArchiveEvidence(context.Background(), actorFor(owner), evidence.ID, current.Revision,
		"Preserve the withdrawn unit for institutional traceability", "archive", fixedNow.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, _ = store.EvidenceByID(context.Background(), evidence.ID)
	if current.State != domain.EvidenceArchived {
		t.Fatalf("archived state=%s", current.State)
	}
	if err := store.RestoreEvidence(context.Background(), actorFor(owner), evidence.ID, current.Revision,
		"The source issued a signed correction and requests renewed review", "restore", fixedNow.Add(13*time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, _ = store.EvidenceByID(context.Background(), evidence.ID)
	restoredVersion, _ := store.VersionByID(context.Background(), version.ID)
	if current.State != domain.EvidenceReviewing || restoredVersion.State != domain.VersionUnderReview {
		t.Fatalf("restored state evidence=%s version=%s", current.State, restoredVersion.State)
	}
	counts, err := store.JobCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts["pending"] != 1 {
		t.Fatalf("restore did not queue integrity check: %#v", counts)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("withdraw/archive/restore audit chain invalid: %v", err)
	}
}

func TestResponsibilityHandoffUsesRevisionAndRecipientOwnership(t *testing.T) {
	store := openTestStore(t)
	from := createUser(t, store, "handoff-from@example.test", domain.RoleResearcher)
	to := createUser(t, store, "handoff-to@example.test", domain.RoleResearcher)
	stranger := createUser(t, store, "handoff-stranger@example.test", domain.RoleResearcher)
	evidence, _ := registerTestEvidence(t, store, from, "handoff")
	handoff, err := store.CreateHandoff(context.Background(), actorFor(from), evidence.ID, to.ID, evidence.Revision,
		"Transfer responsibility during field rotation", "handoff-create", fixedNow.Add(time.Hour), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptHandoff(context.Background(), actorFor(stranger), handoff.ID, "stranger", fixedNow.Add(time.Minute)); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("stranger accepted handoff: %v", err)
	}
	if err := store.AcceptHandoff(context.Background(), actorFor(to), handoff.ID, "recipient", fixedNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated, _ := store.EvidenceByID(context.Background(), evidence.ID)
	if updated.OwnerID != to.ID || updated.Revision != evidence.Revision+1 {
		t.Fatalf("handoff result=%#v", updated)
	}
	if err := store.AcceptHandoff(context.Background(), actorFor(to), handoff.ID, "duplicate", fixedNow.Add(3*time.Minute)); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("duplicate handoff acceptance=%v", err)
	}
}

func prepareSubmittedVersion(t *testing.T, store *Store, author domain.User, fingerprint string) (domain.Evidence, domain.EvidenceVersion) {
	t.Helper()
	evidence, version := registerTestEvidence(t, store, author, fingerprint)
	claim := domain.Claim{Statement: "The evidence contains a sufficiently detailed scientific assertion", Locator: "section 2", Confidence: 0.88}
	if _, err := store.AddClaim(context.Background(), actorFor(author), evidence.ID, version.ID, claim,
		evidence.Revision, "claim", fixedNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	evidence, _ = store.EvidenceByID(context.Background(), evidence.ID)
	version, err := store.SubmitForReview(context.Background(), actorFor(author), evidence.ID, version.ID,
		evidence.Revision, "submit", fixedNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return evidence, version
}

func prepareApprovedVersion(t *testing.T, store *Store, author, reviewer domain.User, fingerprint string) (domain.Evidence, domain.EvidenceVersion) {
	t.Helper()
	evidence, version := prepareSubmittedVersion(t, store, author, fingerprint)
	slot, err := store.ClaimReviewSlot(context.Background(), actorFor(reviewer), version.ID,
		fixedNow.Add(48*time.Hour), "slot", fixedNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideReview(context.Background(), actorFor(reviewer), slot.ID, domain.ReviewApprove,
		"The source and extracted claim are consistent and complete.", "approve", fixedNow.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	version, err = store.VersionByID(context.Background(), version.ID)
	if err != nil {
		t.Fatal(err)
	}
	return evidence, version
}
