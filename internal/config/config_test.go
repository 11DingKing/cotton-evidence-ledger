package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	keys := []string{"COTTON_ADDR", "COTTON_DATABASE", "COTTON_SESSION_TTL", "COTTON_WORKER_INTERVAL",
		"COTTON_WORKER_LEASE", "COTTON_SHUTDOWN_TIMEOUT", "COTTON_BOOTSTRAP_EMAIL",
		"COTTON_BOOTSTRAP_PASSWORD", "COTTON_MAX_BODY_BYTES"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr=%q", cfg.Addr)
	}
	if cfg.DatabasePath != "cotton-evidence.db" {
		t.Errorf("DatabasePath=%q", cfg.DatabasePath)
	}
	if cfg.SessionTTL != 12*time.Hour {
		t.Errorf("SessionTTL=%s", cfg.SessionTTL)
	}
	if cfg.WorkerInterval != 2*time.Second {
		t.Errorf("WorkerInterval=%s", cfg.WorkerInterval)
	}
	if cfg.WorkerLease != 30*time.Second {
		t.Errorf("WorkerLease=%s", cfg.WorkerLease)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout=%s", cfg.ShutdownTimeout)
	}
	if cfg.MaxBodyBytes != 1<<20 {
		t.Errorf("MaxBodyBytes=%d", cfg.MaxBodyBytes)
	}
	if cfg.BootstrapEmail != "owner@example.test" {
		t.Errorf("BootstrapEmail=%q", cfg.BootstrapEmail)
	}
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	t.Setenv("COTTON_ADDR", "127.0.0.1:9090")
	t.Setenv("COTTON_DATABASE", "/tmp/cotton-test.db")
	t.Setenv("COTTON_SESSION_TTL", "45m")
	t.Setenv("COTTON_WORKER_INTERVAL", "150ms")
	t.Setenv("COTTON_WORKER_LEASE", "8s")
	t.Setenv("COTTON_SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("COTTON_BOOTSTRAP_EMAIL", " OWNER@EXAMPLE.TEST ")
	t.Setenv("COTTON_BOOTSTRAP_PASSWORD", "a-secure-test-password")
	t.Setenv("COTTON_MAX_BODY_BYTES", "4096")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load overrides: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" || cfg.DatabasePath != "/tmp/cotton-test.db" {
		t.Fatalf("string overrides not applied: %#v", cfg)
	}
	if cfg.SessionTTL != 45*time.Minute || cfg.WorkerInterval != 150*time.Millisecond ||
		cfg.WorkerLease != 8*time.Second || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("duration overrides not applied: %#v", cfg)
	}
	if cfg.BootstrapEmail != "owner@example.test" || cfg.BootstrapPassword != "a-secure-test-password" {
		t.Fatalf("bootstrap overrides not normalized: %#v", cfg)
	}
	if cfg.MaxBodyBytes != 4096 {
		t.Fatalf("MaxBodyBytes=%d", cfg.MaxBodyBytes)
	}
}

func TestLoadRejectsInvalidDurations(t *testing.T) {
	keys := []string{"COTTON_SESSION_TTL", "COTTON_WORKER_INTERVAL", "COTTON_WORKER_LEASE", "COTTON_SHUTDOWN_TIMEOUT"}
	values := []string{"nonsense", "0s", "-1s"}
	for _, key := range keys {
		for _, value := range values {
			t.Run(key+"_"+value, func(t *testing.T) {
				t.Setenv("COTTON_BOOTSTRAP_PASSWORD", "valid-test-password")
				t.Setenv(key, value)
				_, err := Load()
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("expected %s error for %q, got %v", key, value, err)
				}
			})
		}
	}
}

func TestLoadRejectsInvalidBootstrapValues(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		contains string
	}{
		{"malformed email", "not-an-email", "valid-test-password", "COTTON_BOOTSTRAP_EMAIL"},
		{"short password", "owner@example.test", "too-short", "COTTON_BOOTSTRAP_PASSWORD"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("COTTON_BOOTSTRAP_EMAIL", test.email)
			t.Setenv("COTTON_BOOTSTRAP_PASSWORD", test.password)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("expected %s error, got %v", test.contains, err)
			}
		})
	}
}

func TestLoadRejectsInvalidMaxBodyBytes(t *testing.T) {
	for _, value := range []string{"abc", "-1", "0", "1023"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("COTTON_BOOTSTRAP_PASSWORD", "valid-test-password")
			t.Setenv("COTTON_MAX_BODY_BYTES", value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "COTTON_MAX_BODY_BYTES") {
				t.Fatalf("expected max body error for %q, got %v", value, err)
			}
		})
	}
}
