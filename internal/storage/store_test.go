package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

var fixedNow = time.Date(2026, 8, 26, 10, 30, 0, 0, time.UTC)

func TestMigrationsCreateRelationalSchema(t *testing.T) {
	store := openTestStore(t)
	rows, err := store.db.QueryContext(context.Background(), `
        SELECT name FROM sqlite_master
        WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
        ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	want := []string{"audit_events", "citations", "claims", "evidence_units", "evidence_versions",
		"idempotency_keys", "jobs", "notifications", "responsibility_handoffs", "review_slots",
		"reviews", "schema_migrations", "sessions", "sources", "users"}
	if fmt.Sprint(tables) != fmt.Sprint(want) {
		t.Fatalf("tables=%v, want %v", tables, want)
	}
	var versions int
	if err := store.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Fatalf("migration count=%d, want 2", versions)
	}
}

func TestMigrationIsIdempotentAndDataSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := first.CreateUser(ctx, "collector@example.test", "Collector", "hash", domain.RoleCollector, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	t.Cleanup(func() { second.Close() })
	restored, err := second.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Email != user.Email || restored.Role != domain.RoleCollector || !restored.Active {
		t.Fatalf("restored user=%#v, want %#v", restored, user)
	}
	var versions int
	if err := second.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Fatalf("migrations reapplied incorrectly: %d", versions)
	}
}

func TestForeignKeysAndUniqueConstraintsAreEnforced(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	_, err := store.db.ExecContext(ctx, `
        INSERT INTO sessions(user_id, token_hash, expires_at, created_at, last_seen_at)
        VALUES(999,'missing-user',?,?,?)`, formatTime(fixedNow.Add(time.Hour)), formatTime(fixedNow), formatTime(fixedNow))
	if err == nil {
		t.Fatal("foreign key accepted missing user")
	}
	createUser(t, store, "unique@example.test", domain.RoleCollector)
	_, err = store.CreateUser(ctx, "UNIQUE@example.test", "Duplicate", "hash", domain.RoleCollector, fixedNow)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("case-insensitive email uniqueness returned %v", err)
	}
}

func TestEveryPooledConnectionEnablesForeignKeys(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	connections := make([]*sql.Conn, 0, 8)
	for index := 0; index < 8; index++ {
		connection, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection %d: %v", index, err)
		}
		connections = append(connections, connection)
	}
	defer func() {
		for _, connection := range connections {
			connection.Close()
		}
	}()
	for index, connection := range connections {
		var enabled int
		if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			t.Fatalf("query foreign keys on connection %d: %v", index, err)
		}
		if enabled != 1 {
			t.Fatalf("connection %d has foreign_keys=%d", index, enabled)
		}
	}
}

func TestRegisterEvidenceCommitsSourceVersionAndAuditAtomically(t *testing.T) {
	store := openTestStore(t)
	owner := createUser(t, store, "researcher@example.test", domain.RoleResearcher)
	evidence, version := registerTestEvidence(t, store, owner, "fingerprint-atomic")
	if evidence.SourceID == 0 || evidence.CurrentVersionID == nil || *evidence.CurrentVersionID != version.ID {
		t.Fatalf("evidence/version relation incomplete: %#v %#v", evidence, version)
	}
	if evidence.State != domain.EvidenceRegistered || version.State != domain.VersionDraft {
		t.Fatalf("initial states incorrect: %s %s", evidence.State, version.State)
	}
	page, err := store.AuditEvents(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Action != "evidence.registered" {
		t.Fatalf("registration audit missing: %#v", page.Items)
	}
	if err := store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("audit chain invalid after registration: %v", err)
	}
}

func TestDuplicateEvidenceRollsBackEveryEntity(t *testing.T) {
	store := openTestStore(t)
	owner := createUser(t, store, "owner@example.test", domain.RoleResearcher)
	registerTestEvidence(t, store, owner, "same-fingerprint")
	params := testRegistration(owner, "same-fingerprint")
	_, _, err := store.RegisterEvidence(context.Background(), params)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("duplicate fingerprint error=%v", err)
	}
	for table, want := range map[string]int{"sources": 1, "evidence_units": 1, "evidence_versions": 1, "audit_events": 1} {
		var count int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Errorf("%s count=%d, want %d", table, count, want)
		}
	}
}

func TestAddClaimUsesOptimisticRevision(t *testing.T) {
	store := openTestStore(t)
	owner := createUser(t, store, "claims@example.test", domain.RoleResearcher)
	evidence, version := registerTestEvidence(t, store, owner, "claim-revision")
	actor := actorFor(owner)
	claim := domain.Claim{Statement: "Fiber strength is stable after the treatment", Locator: "page 4 table 2", Confidence: 0.9}
	saved, err := store.AddClaim(context.Background(), actor, evidence.ID, version.ID, claim, evidence.Revision,
		"claim-request", fixedNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("add claim: %v", err)
	}
	if saved.ID == 0 || saved.CreatedBy != owner.ID {
		t.Fatalf("saved claim incomplete: %#v", saved)
	}
	_, err = store.AddClaim(context.Background(), actor, evidence.ID, version.ID,
		domain.Claim{Statement: "A second sufficiently detailed scientific claim", Locator: "page 5", Confidence: 0.8},
		evidence.Revision, "stale-request", fixedNow.Add(2*time.Minute))
	if !errors.Is(err, apperr.ErrVersion) {
		t.Fatalf("stale revision error=%v", err)
	}
	claims, err := store.ClaimsByVersion(context.Background(), version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 {
		t.Fatalf("stale transaction leaked claim: %#v", claims)
	}
}

func TestEvidencePaginationFiltersStateAndOwner(t *testing.T) {
	store := openTestStore(t)
	firstOwner := createUser(t, store, "page-one@example.test", domain.RoleResearcher)
	secondOwner := createUser(t, store, "page-two@example.test", domain.RoleResearcher)
	for index := 0; index < 4; index++ {
		owner := firstOwner
		if index%2 == 1 {
			owner = secondOwner
		}
		registerTestEvidence(t, store, owner, fmt.Sprintf("page-fingerprint-%d", index))
	}
	page, err := store.ListEvidence(context.Background(), domain.EvidenceRegistered, firstOwner.ID, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].OwnerID != firstOwner.ID || page.NextCursor == 0 {
		t.Fatalf("first page incorrect: %#v", page)
	}
	second, err := store.ListEvidence(context.Background(), domain.EvidenceRegistered, firstOwner.ID, page.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID == page.Items[0].ID || second.NextCursor != 0 {
		t.Fatalf("second page incorrect: %#v", second)
	}
}

func TestSessionLifecycleIncludesExpiryAndRevocation(t *testing.T) {
	store := openTestStore(t)
	user := createUser(t, store, "session@example.test", domain.RoleCollector)
	ctx := context.Background()
	sessionID, err := store.CreateSessionAudited(ctx, user.ID, "token-hash", "login-request", fixedNow.Add(time.Hour), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := store.ActorByTokenHash(ctx, "token-hash", fixedNow.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if actor.UserID != user.ID || actor.SessionID != sessionID || actor.Role != domain.RoleCollector {
		t.Fatalf("actor=%#v", actor)
	}
	if err := store.RevokeSessionAudited(ctx, actor, "logout-request", fixedNow.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActorByTokenHash(ctx, "token-hash", fixedNow.Add(32*time.Minute)); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("revoked session authenticated: %v", err)
	}
	_, err = store.CreateSession(ctx, user.ID, "expired-hash", fixedNow.Add(-time.Minute), fixedNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActorByTokenHash(ctx, "expired-hash", fixedNow); !errors.Is(err, apperr.ErrExpired) {
		t.Fatalf("expired session error=%v", err)
	}
	if err := store.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("session audits broke chain: %v", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cotton-test.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	return store
}

func createUser(t *testing.T, store *Store, email string, role domain.Role) domain.User {
	t.Helper()
	user, err := store.CreateUser(context.Background(), email, email, "test-hash", role, fixedNow)
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func actorFor(user domain.User) domain.Actor {
	return domain.Actor{UserID: user.ID, SessionID: user.ID * 100, Email: user.Email, Role: user.Role}
}

func testRegistration(owner domain.User, fingerprint string) RegisterEvidenceParams {
	return RegisterEvidenceParams{
		Source: domain.Source{Kind: domain.SourcePaper, ExternalID: "DOI-" + fingerprint,
			Title: "Cotton fiber evidence " + fingerprint, Origin: "Agronomy archive",
			Fingerprint: fingerprint, SubmitterID: owner.ID},
		Version: domain.EvidenceVersion{Title: "Cotton fiber evidence " + fingerprint,
			Abstract:    "A sufficiently detailed abstract for the cotton evidence integration test.",
			ContentHash: "content-" + fingerprint, CreatedBy: owner.ID},
		OwnerID: owner.ID, RequestID: "register-" + fingerprint, Now: fixedNow,
	}
}

func registerTestEvidence(t *testing.T, store *Store, owner domain.User, fingerprint string) (domain.Evidence, domain.EvidenceVersion) {
	t.Helper()
	evidence, version, err := store.RegisterEvidence(context.Background(), testRegistration(owner, fingerprint))
	if err != nil {
		t.Fatalf("register evidence %s: %v", fingerprint, err)
	}
	return evidence, version
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
