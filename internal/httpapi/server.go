package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/11DingKing/cotton-evidence-ledger/internal/evidence"
	"github.com/11DingKing/cotton-evidence-ledger/internal/identity"
	"github.com/11DingKing/cotton-evidence-ledger/internal/jobs"
	"github.com/11DingKing/cotton-evidence-ledger/internal/middleware"
	"github.com/11DingKing/cotton-evidence-ledger/internal/publication"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type Server struct {
	identity    *identity.Service
	evidence    *evidence.Service
	reviews     *reviews.Service
	publication *publication.Service
	jobs        *jobs.Service
	store       *storage.Store
	logger      *slog.Logger
	maxBody     int64
}

type Dependencies struct {
	Identity    *identity.Service
	Evidence    *evidence.Service
	Reviews     *reviews.Service
	Publication *publication.Service
	Jobs        *jobs.Service
	Store       *storage.Store
	Logger      *slog.Logger
	MaxBody     int64
}

func New(dependencies Dependencies) *Server {
	return &Server{identity: dependencies.Identity, evidence: dependencies.Evidence,
		reviews: dependencies.Reviews, publication: dependencies.Publication,
		jobs: dependencies.Jobs, store: dependencies.Store, logger: dependencies.Logger,
		maxBody: dependencies.MaxBody}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("POST /v1/sessions/login", s.login)

	s.protected(mux, "POST /v1/sessions/logout", s.logout)
	s.protected(mux, "POST /v1/users", s.createUser)
	s.protected(mux, "POST /v1/evidence", s.registerEvidence)
	s.protected(mux, "GET /v1/evidence", s.listEvidence)
	s.protected(mux, "GET /v1/evidence/{evidenceID}", s.getEvidence)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/versions/{versionID}/claims", s.addClaim)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/versions/{versionID}/submit-review", s.submitReview)
	s.protected(mux, "POST /v1/versions/{versionID}/review-slot", s.claimReview)
	s.protected(mux, "POST /v1/review-slots/{slotID}/decision", s.decideReview)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/versions/{versionID}/publish", s.publish)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/corrections", s.startCorrection)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/replace", s.replaceVersion)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/withdraw", s.withdraw)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/archive", s.archive)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/restore", s.restore)
	s.protected(mux, "POST /v1/evidence/{evidenceID}/handoffs", s.createHandoff)
	s.protected(mux, "POST /v1/handoffs/{handoffID}/accept", s.acceptHandoff)
	s.protected(mux, "POST /v1/jobs", s.enqueueJob)
	s.protected(mux, "GET /v1/jobs/counts", s.jobCounts)
	s.protected(mux, "GET /v1/audit", s.listAudit)
	s.protected(mux, "GET /v1/notifications", s.listNotifications)

	handler := middleware.Log(s.logger, mux)
	handler = middleware.Recover(s.logger, handler)
	handler = middleware.RequestID(handler)
	return handler
}

func (s *Server) protected(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.Handle(pattern, middleware.Authenticate(s.identity, handler))
}
