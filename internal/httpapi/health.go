package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "alive"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready", "dependency": "database",
		})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}
