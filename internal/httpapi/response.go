package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/middleware"
)

type errorEnvelope struct {
	Error struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		RequestID string            `json:"request_id"`
		Fields    map[string]string `json:"fields,omitempty"`
	} `json:"error"`
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		s.writeError(w, r, apperr.New("invalid_json", "请求 JSON 无效", errors.Join(apperr.ErrInvalid, err)))
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		s.writeError(w, r, apperr.New("invalid_json", "请求只能包含一个 JSON 值", apperr.ErrInvalid))
		return false
	}
	return true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value != nil {
		if err := json.NewEncoder(w).Encode(value); err != nil {
			s.logger.Error("encode response", "error", err)
		}
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := apperr.Code(err)
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, apperr.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, apperr.ErrUnauthorized), errors.Is(err, apperr.ErrExpired):
		status = http.StatusUnauthorized
	case errors.Is(err, apperr.ErrForbidden), errors.Is(err, apperr.ErrSelfReview):
		status = http.StatusForbidden
	case errors.Is(err, apperr.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, apperr.ErrConflict), errors.Is(err, apperr.ErrVersion),
		errors.Is(err, apperr.ErrQuotaOccupied), errors.Is(err, apperr.ErrInvalidState):
		status = http.StatusConflict
	case errors.Is(err, apperr.ErrUnavailable):
		status = http.StatusServiceUnavailable
	}
	envelope := errorEnvelope{}
	envelope.Error.Code = code
	envelope.Error.Message = apperr.PublicMessage(err)
	envelope.Error.RequestID = audit.RequestID(r.Context())
	var typed *apperr.Error
	if errors.As(err, &typed) {
		envelope.Error.Fields = typed.Fields
	}
	if status >= 500 {
		s.logger.Error("request failed", "request_id", envelope.Error.RequestID, "error", err)
	}
	s.writeJSON(w, status, envelope)
}

func actorFrom(r *http.Request) (domain.Actor, error) {
	actor, ok := middleware.Actor(r.Context())
	if !ok {
		return domain.Actor{}, apperr.ErrUnauthorized
	}
	return actor, nil
}

func pathID(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.PathValue(name))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("path parameter %s must be a positive integer: %w", name, apperr.ErrInvalid)
	}
	return id, nil
}

func queryInt(r *http.Request, name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("query parameter %s must be non-negative: %w", name, apperr.ErrInvalid)
	}
	return value, nil
}
