package storage

import (
	"testing"
	"time"
)

func TestBackoffRetainsExponentialDelay(t *testing.T) {
	if got := backoff(4); got != 8*time.Second {
		t.Fatalf("backoff=%s want 8s", got)
	}
}
