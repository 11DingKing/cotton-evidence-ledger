package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

func TestBuildAndVerifyAuditChain(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 8, 26, 9, 0, 0, 123, time.FixedZone("CST", 8*60*60))
	actorID := int64(42)
	first, err := Build("", Payload{ActorID: &actorID, Action: "evidence.registered", ObjectType: "evidence",
		ObjectID: "100", Result: "success", RequestID: "req-1", Before: nil,
		After: map[string]any{"state": "registered", "revision": 1}, CreatedAt: base})
	if err != nil {
		t.Fatalf("build first event: %v", err)
	}
	if first.PreviousHash != GenesisHash {
		t.Fatalf("first event previous hash=%q, want genesis", first.PreviousHash)
	}
	if len(first.EventHash) != 64 {
		t.Fatalf("first event hash length=%d, want 64", len(first.EventHash))
	}
	second, err := Build(first.EventHash, Payload{ActorID: &actorID, Action: "claim.extracted", ObjectType: "version",
		ObjectID: "200", Result: "success", RequestID: "req-2",
		Before: map[string]any{"claims": 0}, After: map[string]any{"claims": 1}, CreatedAt: base.Add(time.Minute)})
	if err != nil {
		t.Fatalf("build second event: %v", err)
	}
	third, err := Build(second.EventHash, Payload{Action: "review.slot_expired", ObjectType: "review_slot",
		ObjectID: "300", Result: "success", RequestID: "worker", CreatedAt: base.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("build system event: %v", err)
	}
	events := []domain.AuditEvent{first, second, third}
	if err := Verify(events); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
	if !strings.Contains(first.AfterJSON, `"revision":1`) || !strings.Contains(first.AfterJSON, `"state":"registered"`) {
		t.Fatalf("canonical after image missing fields: %s", first.AfterJSON)
	}
	if third.ActorID != nil {
		t.Fatalf("system event should not have actor: %v", third.ActorID)
	}
}

func TestAuditHashChangesForEveryProtectedField(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 26, 1, 2, 3, 4, time.UTC)
	actor := int64(7)
	base, err := Build(GenesisHash, Payload{ActorID: &actor, Action: "review.decided", ObjectType: "version",
		ObjectID: "19", Result: "success", RequestID: "request-a", Before: map[string]any{"state": "under_review"},
		After: map[string]any{"state": "approved"}, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*domain.AuditEvent)
	}{
		{"previous hash", func(event *domain.AuditEvent) { event.PreviousHash = strings.Repeat("a", 64) }},
		{"actor", func(event *domain.AuditEvent) { other := int64(8); event.ActorID = &other }},
		{"action", func(event *domain.AuditEvent) { event.Action = "review.changed" }},
		{"object type", func(event *domain.AuditEvent) { event.ObjectType = "evidence" }},
		{"object id", func(event *domain.AuditEvent) { event.ObjectID = "20" }},
		{"result", func(event *domain.AuditEvent) { event.Result = "denied" }},
		{"request id", func(event *domain.AuditEvent) { event.RequestID = "request-b" }},
		{"before image", func(event *domain.AuditEvent) { event.BeforeJSON = `{"state":"draft"}` }},
		{"after image", func(event *domain.AuditEvent) { event.AfterJSON = `{"state":"published"}` }},
		{"timestamp", func(event *domain.AuditEvent) { event.CreatedAt = event.CreatedAt.Add(time.Nanosecond) }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed := base
			test.mutate(&changed)
			if Hash(changed) == base.EventHash {
				t.Fatalf("hash did not change after mutating %s", test.name)
			}
			changed.EventHash = base.EventHash
			if err := Verify([]domain.AuditEvent{changed}); err == nil {
				t.Fatalf("verification accepted event with changed %s", test.name)
			}
		})
	}
}

func TestVerifyRejectsBrokenLinks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first, _ := Build("", Payload{Action: "one", ObjectType: "test", ObjectID: "1", Result: "success", CreatedAt: now})
	second, _ := Build(first.EventHash, Payload{Action: "two", ObjectType: "test", ObjectID: "2", Result: "success", CreatedAt: now.Add(time.Second)})
	second.PreviousHash = GenesisHash
	second.EventHash = Hash(second)
	if err := Verify([]domain.AuditEvent{first, second}); err == nil || !strings.Contains(err.Error(), "links to") {
		t.Fatalf("expected link error, got %v", err)
	}
	if err := Verify(nil); err != nil {
		t.Fatalf("empty audit chain should be valid: %v", err)
	}
}

func TestBuildRejectsUnencodableImages(t *testing.T) {
	t.Parallel()
	_, err := Build("", Payload{Action: "bad", ObjectType: "test", ObjectID: "1", Result: "failed",
		Before: make(chan int), CreatedAt: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "before image") {
		t.Fatalf("expected before image encoding error, got %v", err)
	}
	_, err = Build("", Payload{Action: "bad", ObjectType: "test", ObjectID: "1", Result: "failed",
		Before: map[string]any{}, After: func() {}, CreatedAt: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "after image") {
		t.Fatalf("expected after image encoding error, got %v", err)
	}
}

func TestRequestIDContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if RequestID(ctx) != "" {
		t.Fatal("background context unexpectedly has request id")
	}
	ctx = WithRequestID(ctx, "request-123")
	if got := RequestID(ctx); got != "request-123" {
		t.Fatalf("RequestID=%q, want request-123", got)
	}
	child := WithRequestID(ctx, "request-456")
	if RequestID(child) != "request-456" || RequestID(ctx) != "request-123" {
		t.Fatal("request id context values were not isolated")
	}
}
