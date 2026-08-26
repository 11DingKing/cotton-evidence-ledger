package httpapi

import (
	"net/http"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func (s *Server) submitReview(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	evidenceID, err := pathID(r, "evidenceID")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	versionID, err := pathID(r, "versionID")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var input struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	version, err := s.reviews.Submit(r.Context(), actor, evidenceID, versionID, input.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, version)
}

func (s *Server) claimReview(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	versionID, err := pathID(r, "versionID")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var input struct {
		DueAt *time.Time `json:"due_at"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	slot, err := s.reviews.Claim(r.Context(), actor, versionID, input.DueAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, slot)
}

func (s *Server) decideReview(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	slotID, err := pathID(r, "slotID")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var input struct {
		Decision domain.ReviewDecision `json:"decision"`
		Opinion  string                `json:"opinion"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	review, err := s.reviews.Decide(r.Context(), actor, slotID, input.Decision, input.Opinion)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, review)
}
