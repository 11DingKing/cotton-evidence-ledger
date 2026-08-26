package httpapi

import (
	"net/http"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func (s *Server) enqueueJob(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var input struct {
		Kind       string         `json:"kind"`
		ObjectType string         `json:"object_type"`
		ObjectID   int64          `json:"object_id"`
		Payload    map[string]any `json:"payload"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	job, err := s.jobs.Enqueue(r.Context(), actor, input.Kind, input.ObjectType, input.ObjectID, input.Payload)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) jobCounts(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	counts, err := s.jobs.Counts(r.Context(), actor)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, counts)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if actor.Role != domain.RoleKnowledgeOwner {
		s.writeError(w, r, apperr.ErrForbidden)
		return
	}
	afterID, err := queryInt(r, "after_id", 0)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.store.AuditEvents(r.Context(), afterID, int(limit))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, page)
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
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
	limit, err := queryInt(r, "limit", 25)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, next, err := s.store.ListNotifications(r.Context(), actor.UserID, afterID, int(limit))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}
