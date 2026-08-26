package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/identity"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(audit.WithRequestID(r.Context(), requestID)))
	})
}

func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("panic recovered", "request_id", audit.RequestID(r.Context()),
					"method", r.Method, "path", r.URL.Path, "panic", recovered, "stack", string(debug.Stack()))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"服务处理请求时发生错误"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func Log(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		capture := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		logger.Info("http request", "request_id", audit.RequestID(r.Context()), "method", r.Method,
			"path", r.URL.Path, "status", capture.status, "bytes", capture.bytes,
			"duration_ms", time.Since(started).Milliseconds())
	})
}

func Authenticate(service *identity.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeUnauthorized(w, audit.RequestID(r.Context()))
			return
		}
		actor, err := service.Authenticate(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			writeUnauthorized(w, audit.RequestID(r.Context()))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	count, err := w.ResponseWriter.Write(data)
	w.bytes += count
	return count, err
}

func newRequestID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(raw)
}

func writeUnauthorized(w http.ResponseWriter, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"请先登录","request_id":"` + requestID + `"}}`))
}
