package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/config"
	"github.com/11DingKing/cotton-evidence-ledger/internal/evidence"
	"github.com/11DingKing/cotton-evidence-ledger/internal/httpapi"
	"github.com/11DingKing/cotton-evidence-ledger/internal/identity"
	"github.com/11DingKing/cotton-evidence-ledger/internal/jobs"
	"github.com/11DingKing/cotton-evidence-ledger/internal/publication"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
	"github.com/11DingKing/cotton-evidence-ledger/internal/worker"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := storage.Open(rootCtx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open application database: %w", err)
	}
	defer store.Close()
	identityService := identity.New(store, cfg.SessionTTL)
	owner, err := identityService.Bootstrap(rootCtx, cfg.BootstrapEmail, cfg.BootstrapPassword)
	if err != nil {
		return fmt.Errorf("bootstrap application owner: %w", err)
	}
	logger.Info("bootstrap owner ready", "user_id", owner.ID, "email", owner.Email)
	evidenceService := evidence.New(store)
	reviewService := reviews.New(store)
	publicationService := publication.New(store)
	jobService := jobs.New(store)
	dispatcher := worker.NewDispatcher(store, reviewService)
	workerID, _ := os.Hostname()
	if strings.TrimSpace(workerID) == "" {
		workerID = "cotton-worker"
	}
	runner := worker.New(store, dispatcher, logger, workerID, cfg.WorkerInterval, cfg.WorkerLease)
	workerCtx, cancelWorker := context.WithCancel(rootCtx)
	go runner.Run(workerCtx)
	api := httpapi.New(httpapi.Dependencies{Identity: identityService, Evidence: evidenceService,
		Reviews: reviewService, Publication: publicationService, Jobs: jobService,
		Store: store, Logger: logger, MaxBody: cfg.MaxBodyBytes})
	server := &http.Server{Addr: cfg.Addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "address", cfg.Addr)
		serveErrors <- server.ListenAndServe()
	}()
	select {
	case <-rootCtx.Done():
		logger.Info("shutdown requested")
	case serveErr := <-serveErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			cancelWorker()
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		cancelWorker()
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	cancelWorker()
	runner.Wait()
	logger.Info("shutdown complete")
	return nil
}

func healthcheck() error {
	addr := os.Getenv("COTTON_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host := addr
	if strings.HasPrefix(addr, ":") {
		host = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + host + "/health/ready")
	if err != nil {
		return fmt.Errorf("readiness request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned HTTP %d", response.StatusCode)
	}
	return nil
}
