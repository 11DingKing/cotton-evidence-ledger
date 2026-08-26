package domain

import (
	"errors"
	"testing"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
)

func TestEvidenceStateTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		from    EvidenceState
		to      EvidenceState
		allowed bool
	}{
		{"registered enters deduplication", EvidenceRegistered, EvidenceDeduplicating, true},
		{"registered can be archived", EvidenceRegistered, EvidenceArchived, true},
		{"registered cannot publish", EvidenceRegistered, EvidencePublished, false},
		{"deduplication enters extraction", EvidenceDeduplicating, EvidenceExtracting, true},
		{"deduplication can be archived", EvidenceDeduplicating, EvidenceArchived, true},
		{"deduplication cannot review", EvidenceDeduplicating, EvidenceReviewing, false},
		{"extraction enters review", EvidenceExtracting, EvidenceReviewing, true},
		{"extraction can be archived", EvidenceExtracting, EvidenceArchived, true},
		{"extraction cannot publish directly", EvidenceExtracting, EvidencePublished, false},
		{"review publishes", EvidenceReviewing, EvidencePublished, true},
		{"review requests new extraction", EvidenceReviewing, EvidenceExtracting, true},
		{"review can be archived", EvidenceReviewing, EvidenceArchived, true},
		{"published starts correction", EvidencePublished, EvidenceCorrecting, true},
		{"published can be withdrawn", EvidencePublished, EvidenceWithdrawn, true},
		{"published cannot return to registered", EvidencePublished, EvidenceRegistered, false},
		{"correction returns to review", EvidenceCorrecting, EvidenceReviewing, true},
		{"correction can be withdrawn", EvidenceCorrecting, EvidenceWithdrawn, true},
		{"withdrawn can be reviewed for restore", EvidenceWithdrawn, EvidenceReviewing, true},
		{"withdrawn can be archived", EvidenceWithdrawn, EvidenceArchived, true},
		{"archived can be restored to review", EvidenceArchived, EvidenceReviewing, true},
		{"archived cannot publish directly", EvidenceArchived, EvidencePublished, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.from.Transition(test.to)
			if test.allowed && err != nil {
				t.Fatalf("expected transition %s -> %s to succeed: %v", test.from, test.to, err)
			}
			if !test.allowed && !errors.Is(err, apperr.ErrInvalidState) {
				t.Fatalf("expected invalid state for %s -> %s, got %v", test.from, test.to, err)
			}
			if got := test.from.CanTransition(test.to); got != test.allowed {
				t.Fatalf("CanTransition(%s,%s)=%v, want %v", test.from, test.to, got, test.allowed)
			}
		})
	}
}

func TestEvidenceStateValidity(t *testing.T) {
	t.Parallel()
	valid := []EvidenceState{EvidenceRegistered, EvidenceDeduplicating, EvidenceExtracting,
		EvidenceReviewing, EvidencePublished, EvidenceCorrecting, EvidenceWithdrawn, EvidenceArchived}
	for _, state := range valid {
		if !state.Valid() {
			t.Errorf("expected %q to be valid", state)
		}
	}
	invalid := []EvidenceState{"", "pending", "deleted", "PUBLISHED", "review"}
	for _, state := range invalid {
		if state.Valid() {
			t.Errorf("expected %q to be invalid", state)
		}
		if err := state.Transition(EvidenceRegistered); !errors.Is(err, apperr.ErrInvalid) {
			t.Errorf("invalid source state %q should return invalid input, got %v", state, err)
		}
	}
}

func TestVersionStateTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from VersionState
		to   VersionState
		ok   bool
	}{
		{VersionDraft, VersionUnderReview, true},
		{VersionDraft, VersionPublished, false},
		{VersionUnderReview, VersionApproved, true},
		{VersionUnderReview, VersionDraft, true},
		{VersionUnderReview, VersionPublished, false},
		{VersionApproved, VersionPublished, true},
		{VersionApproved, VersionDraft, true},
		{VersionPublished, VersionSuperseded, true},
		{VersionPublished, VersionWithdrawn, true},
		{VersionPublished, VersionDraft, false},
		{VersionSuperseded, VersionDraft, false},
		{VersionSuperseded, VersionPublished, false},
		{VersionWithdrawn, VersionUnderReview, true},
		{VersionWithdrawn, VersionPublished, false},
	}
	for _, test := range tests {
		name := string(test.from) + "_to_" + string(test.to)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := test.from.Transition(test.to)
			if test.ok && err != nil {
				t.Fatalf("expected transition to succeed: %v", err)
			}
			if !test.ok && !errors.Is(err, apperr.ErrInvalidState) {
				t.Fatalf("expected invalid state, got %v", err)
			}
		})
	}
}

func TestRolesExposeBusinessCapabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role     Role
		register bool
		extract  bool
		review   bool
		publish  bool
		correct  bool
		withdraw bool
	}{
		{RoleCollector, true, false, false, false, false, false},
		{RoleResearcher, true, true, false, false, true, false},
		{RoleReviewer, false, false, true, false, false, false},
		{RoleKnowledgeOwner, true, true, true, true, true, true},
	}
	for _, test := range tests {
		t.Run(string(test.role), func(t *testing.T) {
			t.Parallel()
			if !test.role.Valid() {
				t.Fatalf("configured role %q must be valid", test.role)
			}
			assertBool(t, "register", test.role.CanRegisterSource(), test.register)
			assertBool(t, "extract", test.role.CanExtractClaims(), test.extract)
			assertBool(t, "review", test.role.CanReview(), test.review)
			assertBool(t, "publish", test.role.CanPublish(), test.publish)
			assertBool(t, "correct", test.role.CanCorrect(), test.correct)
			assertBool(t, "withdraw", test.role.CanWithdraw(), test.withdraw)
		})
	}
	for _, role := range []Role{"", "admin", "owner", "collector_reviewer"} {
		if role.Valid() {
			t.Errorf("unknown role %q reported valid", role)
		}
	}
}

func TestRolesReturnsIndependentSlice(t *testing.T) {
	t.Parallel()
	first := Roles()
	second := Roles()
	if len(first) != 4 || len(second) != 4 {
		t.Fatalf("expected four roles, got %d and %d", len(first), len(second))
	}
	first[0] = "mutated"
	if second[0] != RoleCollector {
		t.Fatalf("Roles returned shared backing array: %q", second[0])
	}
}

func TestReviewDecisionValidity(t *testing.T) {
	t.Parallel()
	if !ReviewApprove.Valid() || !ReviewRequestChanges.Valid() {
		t.Fatal("documented decisions must be valid")
	}
	for _, decision := range []ReviewDecision{"", "reject", "approved", "changes"} {
		if decision.Valid() {
			t.Errorf("unexpected valid decision %q", decision)
		}
	}
}

func assertBool(t *testing.T, name string, got, want bool) {
	t.Helper()
	if got != want {
		t.Fatalf("%s=%v, want %v", name, got, want)
	}
}
