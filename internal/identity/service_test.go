package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

func TestPasswordHashingUsesBcryptAndRejectsInvalidLengths(t *testing.T) {
	t.Parallel()
	hash, err := hashPassword("a-secure-password-for-tests")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "a-secure-password-for-tests" || len(hash) < 50 {
		t.Fatalf("unexpected password hash %q", hash)
	}
	if !verifyPassword(hash, "a-secure-password-for-tests") {
		t.Fatal("correct password did not verify")
	}
	if verifyPassword(hash, "different-secure-password") {
		t.Fatal("incorrect password verified")
	}
	for _, password := range []string{"", "short", "12345678901"} {
		if _, err := hashPassword(password); err == nil {
			t.Errorf("short password %q accepted", password)
		}
	}
	tooLong := make([]byte, 257)
	for index := range tooLong {
		tooLong[index] = 'x'
	}
	if _, err := hashPassword(string(tooLong)); err == nil {
		t.Fatal("overlong password accepted")
	}
}

func TestTokenGenerationReturnsOpaqueTokenAndStableHash(t *testing.T) {
	t.Parallel()
	first, firstHash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || firstHash == secondHash {
		t.Fatal("random tokens were reused")
	}
	if len(first) < 40 || len(firstHash) != 64 {
		t.Fatalf("token lengths token=%d hash=%d", len(first), len(firstHash))
	}
	if tokenHash(first) != firstHash {
		t.Fatal("token hash is not stable")
	}
	if tokenHash(first) == first {
		t.Fatal("stored token hash exposes bearer token")
	}
}

func TestLoginAuthenticateLogoutLifecycle(t *testing.T) {
	store := openIdentityStore(t)
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	service := New(store, time.Hour).WithClock(func() time.Time { return now })
	owner, err := service.Bootstrap(context.Background(), "OWNER@example.test", "owner-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if owner.Email != "owner@example.test" || owner.Role != domain.RoleKnowledgeOwner {
		t.Fatalf("owner=%#v", owner)
	}
	ctx := audit.WithRequestID(context.Background(), "login-request")
	login, err := service.Login(ctx, " owner@example.test ", "owner-test-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.Token == "" || !login.ExpiresAt.Equal(now.Add(time.Hour)) || login.User.ID != owner.ID {
		t.Fatalf("login result=%#v", login)
	}
	actor, err := service.Authenticate(context.Background(), login.Token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if actor.UserID != owner.ID || actor.Role != domain.RoleKnowledgeOwner || actor.SessionID == 0 {
		t.Fatalf("actor=%#v", actor)
	}
	logoutCtx := audit.WithRequestID(context.Background(), "logout-request")
	if err := service.Logout(logoutCtx, actor); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), login.Token); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("revoked token authenticated: %v", err)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("login/logout audits invalid: %v", err)
	}
}

func TestLoginRejectsUnknownWrongPasswordAndDisabledUser(t *testing.T) {
	store := openIdentityStore(t)
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	service := New(store, time.Hour).WithClock(func() time.Time { return now })
	owner, err := service.Bootstrap(context.Background(), "owner@example.test", "owner-test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, credentials := range []struct{ email, password string }{
		{"missing@example.test", "owner-test-password"},
		{"owner@example.test", "wrong-test-password"},
		{"", ""},
	} {
		if _, err := service.Login(context.Background(), credentials.email, credentials.password); !errors.Is(err, apperr.ErrUnauthorized) {
			t.Errorf("credentials %#v returned %v", credentials, err)
		}
	}
	if err := store.SetUserActive(context.Background(), owner.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), owner.Email, "owner-test-password"); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("disabled owner logged in: %v", err)
	}
}

func TestExpiredTokenAndCleanup(t *testing.T) {
	store := openIdentityStore(t)
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	clock := now
	service := New(store, 30*time.Minute).WithClock(func() time.Time { return clock })
	if _, err := service.Bootstrap(context.Background(), "owner@example.test", "owner-test-password"); err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(context.Background(), "owner@example.test", "owner-test-password")
	if err != nil {
		t.Fatal(err)
	}
	clock = now.Add(30 * time.Minute)
	if _, err := service.Authenticate(context.Background(), login.Token); !errors.Is(err, apperr.ErrExpired) {
		t.Fatalf("token at expiry boundary returned %v", err)
	}
	clock = now.Add(25 * time.Hour)
	deleted, err := service.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("cleanup deleted=%d, want 1", deleted)
	}
}

func TestCreateUserAndDisableUserRequireKnowledgeOwner(t *testing.T) {
	store := openIdentityStore(t)
	service := New(store, time.Hour)
	owner, err := service.Bootstrap(context.Background(), "owner@example.test", "owner-test-password")
	if err != nil {
		t.Fatal(err)
	}
	ownerActor := domain.Actor{UserID: owner.ID, Role: owner.Role}
	collector, err := service.CreateUser(context.Background(), ownerActor, "collector@example.test", "Collector",
		"collector-password", domain.RoleCollector)
	if err != nil {
		t.Fatal(err)
	}
	collectorActor := domain.Actor{UserID: collector.ID, Role: collector.Role}
	if _, err := service.CreateUser(context.Background(), collectorActor, "reviewer@example.test", "Reviewer",
		"reviewer-password", domain.RoleReviewer); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("collector created user: %v", err)
	}
	if _, err := service.CreateUser(context.Background(), ownerActor, "bad@example.test", "Bad",
		"valid-test-password", "administrator"); !errors.Is(err, apperr.ErrInvalid) {
		t.Fatalf("invalid role error=%v", err)
	}
	if err := service.DisableUser(context.Background(), collectorActor, owner.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("collector disabled owner: %v", err)
	}
	if err := service.DisableUser(context.Background(), ownerActor, owner.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("owner disabled self: %v", err)
	}
	if err := service.DisableUser(context.Background(), ownerActor, collector.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := store.UserByID(context.Background(), collector.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Active {
		t.Fatal("disabled user remained active")
	}
}

func openIdentityStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
