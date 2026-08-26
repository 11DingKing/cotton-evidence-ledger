package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr              string
	DatabasePath      string
	SessionTTL        time.Duration
	WorkerInterval    time.Duration
	WorkerLease       time.Duration
	ShutdownTimeout   time.Duration
	BootstrapEmail    string
	BootstrapPassword string
	MaxBodyBytes      int64
}

func Load() (Config, error) {
	cfg := Config{
		Addr:              env("COTTON_ADDR", ":8080"),
		DatabasePath:      env("COTTON_DATABASE", "cotton-evidence.db"),
		BootstrapEmail:    strings.ToLower(strings.TrimSpace(env("COTTON_BOOTSTRAP_EMAIL", "owner@example.test"))),
		BootstrapPassword: env("COTTON_BOOTSTRAP_PASSWORD", "change-this-password"),
		MaxBodyBytes:      1 << 20,
	}
	var err error
	if cfg.SessionTTL, err = duration("COTTON_SESSION_TTL", 12*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerInterval, err = duration("COTTON_WORKER_INTERVAL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease, err = duration("COTTON_WORKER_LEASE", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("COTTON_SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("COTTON_MAX_BODY_BYTES")); raw != "" {
		cfg.MaxBodyBytes, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cfg.MaxBodyBytes < 1024 {
			return Config{}, fmt.Errorf("COTTON_MAX_BODY_BYTES must be an integer >= 1024")
		}
	}
	if cfg.BootstrapEmail == "" || !strings.Contains(cfg.BootstrapEmail, "@") {
		return Config{}, fmt.Errorf("COTTON_BOOTSTRAP_EMAIL must be an email address")
	}
	if len(cfg.BootstrapPassword) < 12 {
		return Config{}, fmt.Errorf("COTTON_BOOTSTRAP_PASSWORD must contain at least 12 characters")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}
