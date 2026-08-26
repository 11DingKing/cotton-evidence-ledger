package domain

import (
	"errors"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
)

type EvidenceState string

const (
	EvidenceRegistered    EvidenceState = "registered"
	EvidenceDeduplicating EvidenceState = "deduplicating"
	EvidenceExtracting    EvidenceState = "extracting"
	EvidenceReviewing     EvidenceState = "reviewing"
	EvidencePublished     EvidenceState = "published"
	EvidenceCorrecting    EvidenceState = "correcting"
	EvidenceWithdrawn     EvidenceState = "withdrawn"
	EvidenceArchived      EvidenceState = "archived"
)

var evidenceTransitions = map[EvidenceState]map[EvidenceState]bool{
	EvidenceRegistered:    {EvidenceDeduplicating: true, EvidenceArchived: true},
	EvidenceDeduplicating: {EvidenceExtracting: true, EvidenceArchived: true},
	EvidenceExtracting:    {EvidenceReviewing: true, EvidenceArchived: true},
	EvidenceReviewing:     {EvidencePublished: true, EvidenceExtracting: true, EvidenceArchived: true},
	EvidencePublished:     {EvidenceCorrecting: true, EvidenceWithdrawn: true},
	EvidenceCorrecting:    {EvidenceReviewing: true, EvidenceWithdrawn: true},
	EvidenceWithdrawn:     {EvidenceReviewing: true, EvidenceArchived: true},
	EvidenceArchived:      {EvidenceReviewing: true},
}

func (s EvidenceState) Valid() bool {
	_, ok := evidenceTransitions[s]
	return ok
}

func (s EvidenceState) CanTransition(next EvidenceState) bool {
	return evidenceTransitions[s][next]
}

func (s EvidenceState) Transition(next EvidenceState) error {
	if !s.Valid() || !next.Valid() {
		return errors.Join(apperr.ErrInvalid, apperr.ErrInvalidState)
	}
	if !s.CanTransition(next) {
		return errors.Join(apperr.ErrConflict, apperr.ErrInvalidState)
	}
	return nil
}

type VersionState string

const (
	VersionDraft       VersionState = "draft"
	VersionUnderReview VersionState = "under_review"
	VersionApproved    VersionState = "approved"
	VersionPublished   VersionState = "published"
	VersionSuperseded  VersionState = "superseded"
	VersionWithdrawn   VersionState = "withdrawn"
)

var versionTransitions = map[VersionState]map[VersionState]bool{
	VersionDraft:       {VersionUnderReview: true},
	VersionUnderReview: {VersionApproved: true, VersionDraft: true},
	VersionApproved:    {VersionPublished: true, VersionDraft: true},
	VersionPublished:   {VersionSuperseded: true, VersionWithdrawn: true},
	VersionSuperseded:  {},
	VersionWithdrawn:   {VersionUnderReview: true},
}

func (s VersionState) Valid() bool {
	_, ok := versionTransitions[s]
	return ok
}

func (s VersionState) Transition(next VersionState) error {
	allowed, ok := versionTransitions[s]
	if !ok || !next.Valid() {
		return errors.Join(apperr.ErrInvalid, apperr.ErrInvalidState)
	}
	if !allowed[next] {
		return errors.Join(apperr.ErrConflict, apperr.ErrInvalidState)
	}
	return nil
}

type ReviewDecision string

const (
	ReviewApprove        ReviewDecision = "approve"
	ReviewRequestChanges ReviewDecision = "request_changes"
)

func (d ReviewDecision) Valid() bool {
	return d == ReviewApprove || d == ReviewRequestChanges
}
