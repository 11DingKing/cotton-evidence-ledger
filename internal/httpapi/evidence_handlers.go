package httpapi

import (
	"net/http"
	"strconv"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/evidence"
)

func (s *Server) registerEvidence(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var input evidence.RegisterInput
	if !s.decode(w, r, &input) {
		return
	}
	result, err := s.evidence.RegisterIdempotent(r.Context(), actor, input, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, result)
}

func (s *Server) listEvidence(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	afterID, err := queryInt(r, "after_id", 0)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	ownerID, err := queryInt(r, "owner_id", 0)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limitValue, err := queryInt(r, "limit", 25)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.evidence.List(r.Context(), actor, domain.EvidenceState(r.URL.Query().Get("state")),
		ownerID, afterID, int(limitValue))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, page)
}

func (s *Server) getEvidence(w http.ResponseWriter, r *http.Request) {
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
	item, version, claims, err := s.evidence.Get(r.Context(), actor, evidenceID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"evidence": item, "version": version, "claims": claims})
}

func (s *Server) addClaim(w http.ResponseWriter, r *http.Request) {
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
		Statement        string  `json:"statement"`
		Locator          string  `json:"locator"`
		Confidence       float64 `json:"confidence"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	claim, err := s.evidence.AddClaim(r.Context(), actor, evidenceID, versionID, input.ExpectedRevision,
		domain.Claim{Statement: input.Statement, Locator: input.Locator, Confidence: input.Confidence})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/evidence/"+strconv.FormatInt(evidenceID, 10))
	s.writeJSON(w, http.StatusCreated, claim)
}
