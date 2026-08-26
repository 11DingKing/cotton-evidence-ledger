package httpapi

import (
	"net/http"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/publication"
)

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
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
		ExpectedRevision int64   `json:"expected_revision"`
		CitationTargets  []int64 `json:"citation_targets"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	version, err := s.publication.Publish(r.Context(), actor, evidenceID, versionID,
		input.ExpectedRevision, input.CitationTargets)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, version)
}

func (s *Server) startCorrection(w http.ResponseWriter, r *http.Request) {
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
	var input publication.CorrectionInput
	if !s.decode(w, r, &input) {
		return
	}
	input.EvidenceID = evidenceID
	version, err := s.publication.StartCorrection(r.Context(), actor, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, version)
}

func (s *Server) replaceVersion(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		OldVersionID     int64 `json:"old_version_id"`
		NewVersionID     int64 `json:"new_version_id"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	version, err := s.publication.Replace(r.Context(), actor, evidenceID, input.OldVersionID,
		input.NewVersionID, input.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, version)
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if err := s.publication.Withdraw(r.Context(), actor, evidenceID, input.ExpectedRevision, input.Reason); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) archive(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if err := s.publication.Archive(r.Context(), actor, evidenceID, input.ExpectedRevision, input.Reason); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if err := s.publication.Restore(r.Context(), actor, evidenceID, input.ExpectedRevision, input.Reason); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) createHandoff(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		ToUserID         int64     `json:"to_user_id"`
		ExpectedRevision int64     `json:"expected_revision"`
		Reason           string    `json:"reason"`
		ExpiresAt        time.Time `json:"expires_at"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	handoff, err := s.publication.CreateHandoff(r.Context(), actor, evidenceID, input.ToUserID,
		input.ExpectedRevision, input.Reason, input.ExpiresAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, handoff)
}

func (s *Server) acceptHandoff(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	handoffID, err := pathID(r, "handoffID")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.publication.AcceptHandoff(r.Context(), actor, handoffID); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusNoContent, nil)
}
