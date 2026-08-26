package httpapi

import (
	"net/http"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	result, err := s.identity.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, result)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.identity.Logout(r.Context(), actor); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var input struct {
		Email    string      `json:"email"`
		Name     string      `json:"name"`
		Password string      `json:"password"`
		Role     domain.Role `json:"role"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	user, err := s.identity.CreateUser(r.Context(), actor, input.Email, input.Name, input.Password, input.Role)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, user)
}
