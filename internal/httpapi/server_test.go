package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/evidence"
	"github.com/11DingKing/cotton-evidence-ledger/internal/identity"
	"github.com/11DingKing/cotton-evidence-ledger/internal/jobs"
	"github.com/11DingKing/cotton-evidence-ledger/internal/publication"
	"github.com/11DingKing/cotton-evidence-ledger/internal/reviews"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type apiFixture struct {
	server *httptest.Server
	store  *storage.Store
	client *http.Client
}

func TestHealthAndAuthenticationContract(t *testing.T) {
	fixture := newAPIFixture(t)
	for _, path := range []string{"/health/live", "/health/ready"} {
		response := fixture.do(t, http.MethodGet, path, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.StatusCode, readBody(t, response))
		}
		if response.Header.Get("X-Request-ID") == "" {
			t.Fatalf("GET %s missing request id", path)
		}
		response.Body.Close()
	}
	response := fixture.do(t, http.MethodGet, "/v1/evidence", "", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var unauthorized errorEnvelope
	decodeResponse(t, response, &unauthorized)
	if unauthorized.Error.Code != "unauthorized" || unauthorized.Error.Message != "请先登录" || unauthorized.Error.RequestID == "" {
		t.Fatalf("unauthorized envelope=%#v", unauthorized)
	}
	response = fixture.doRaw(t, http.MethodPost, "/v1/sessions/login", "", `{bad json`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid json status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var invalid errorEnvelope
	decodeResponse(t, response, &invalid)
	if invalid.Error.Code != "invalid_json" || invalid.Error.RequestID == "" {
		t.Fatalf("invalid envelope=%#v", invalid)
	}
}

func TestHTTPWorkflowFromRegistrationThroughPublication(t *testing.T) {
	fixture := newAPIFixture(t)
	ownerToken := fixture.login(t, "owner@example.test", "owner-test-password")
	researcher := fixture.createUser(t, ownerToken, map[string]any{
		"email": "researcher@example.test", "name": "Field Researcher",
		"password": "researcher-password", "role": domain.RoleResearcher,
	})
	reviewer := fixture.createUser(t, ownerToken, map[string]any{
		"email": "reviewer@example.test", "name": "Independent Reviewer",
		"password": "reviewer-password", "role": domain.RoleReviewer,
	})
	if researcher.Role != domain.RoleResearcher || reviewer.Role != domain.RoleReviewer {
		t.Fatalf("created roles researcher=%s reviewer=%s", researcher.Role, reviewer.Role)
	}
	researcherToken := fixture.login(t, researcher.Email, "researcher-password")
	reviewerToken := fixture.login(t, reviewer.Email, "reviewer-password")
	register := map[string]any{
		"kind":         domain.SourcePaper,
		"external_id":  "10.1000/cotton-http-1",
		"title":        "Cotton fiber stability under controlled irrigation",
		"origin":       "National cotton agronomy journal",
		"abstract":     "This controlled study compares cotton fiber strength across three irrigation treatments and reports stable measurements.",
		"content_hash": "http-workflow-content-hash",
	}
	response := fixture.do(t, http.MethodPost, "/v1/evidence", researcherToken, register)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var registered evidence.RegisterResult
	decodeResponse(t, response, &registered)
	if registered.Evidence.ID == 0 || registered.Version.ID == 0 || registered.Evidence.Revision != 1 {
		t.Fatalf("registration=%#v", registered)
	}
	claimPath := "/v1/evidence/" + number(registered.Evidence.ID) + "/versions/" + number(registered.Version.ID) + "/claims"
	response = fixture.do(t, http.MethodPost, claimPath, researcherToken, map[string]any{
		"expected_revision": 1,
		"statement":         "The measured fiber strength remains stable across all controlled irrigation treatments",
		"locator":           "results table 3",
		"confidence":        0.94,
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("claim status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var claim domain.Claim
	decodeResponse(t, response, &claim)
	if claim.ID == 0 || claim.VersionID != registered.Version.ID {
		t.Fatalf("claim=%#v", claim)
	}
	submitPath := "/v1/evidence/" + number(registered.Evidence.ID) + "/versions/" + number(registered.Version.ID) + "/submit-review"
	response = fixture.do(t, http.MethodPost, submitPath, researcherToken, map[string]any{"expected_revision": 2})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var submitted domain.EvidenceVersion
	decodeResponse(t, response, &submitted)
	if submitted.State != domain.VersionUnderReview {
		t.Fatalf("submitted state=%s", submitted.State)
	}
	response = fixture.do(t, http.MethodPost, "/v1/versions/"+number(registered.Version.ID)+"/review-slot",
		researcherToken, map[string]any{})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("submitter self-review status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = fixture.do(t, http.MethodPost, "/v1/versions/"+number(registered.Version.ID)+"/review-slot",
		reviewerToken, map[string]any{})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("claim review status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var slot domain.ReviewSlot
	decodeResponse(t, response, &slot)
	response = fixture.do(t, http.MethodPost, "/v1/review-slots/"+number(slot.ID)+"/decision", reviewerToken,
		map[string]any{"decision": domain.ReviewApprove, "opinion": "The measurements and source locators support the extracted conclusion."})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("review decision status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var review domain.Review
	decodeResponse(t, response, &review)
	if review.Decision != domain.ReviewApprove || review.ReviewerID != reviewer.ID {
		t.Fatalf("review=%#v", review)
	}
	publishPath := "/v1/evidence/" + number(registered.Evidence.ID) + "/versions/" + number(registered.Version.ID) + "/publish"
	response = fixture.do(t, http.MethodPost, publishPath, ownerToken,
		map[string]any{"expected_revision": 4, "citation_targets": []int64{}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var published domain.EvidenceVersion
	decodeResponse(t, response, &published)
	if published.State != domain.VersionPublished || published.PublishedAt == nil {
		t.Fatalf("published=%#v", published)
	}
	response = fixture.do(t, http.MethodGet, "/v1/evidence/"+number(registered.Evidence.ID), researcherToken, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get published status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var detail struct {
		Evidence domain.Evidence        `json:"evidence"`
		Version  domain.EvidenceVersion `json:"version"`
		Claims   []domain.Claim         `json:"claims"`
	}
	decodeResponse(t, response, &detail)
	if detail.Evidence.State != domain.EvidencePublished || detail.Version.ID != published.ID || len(detail.Claims) != 1 {
		t.Fatalf("published detail=%#v", detail)
	}
	response = fixture.do(t, http.MethodGet, "/v1/audit?limit=100", ownerToken, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("audit status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var auditPage domain.Page[domain.AuditEvent]
	decodeResponse(t, response, &auditPage)
	if len(auditPage.Items) < 8 {
		t.Fatalf("expected lifecycle audit trail, got %d events", len(auditPage.Items))
	}
	if err := fixture.store.VerifyAuditChain(context.Background()); err != nil {
		t.Fatalf("HTTP lifecycle audit chain invalid: %v", err)
	}
}

func TestHTTPConflictErrorsAndLogoutRevocation(t *testing.T) {
	fixture := newAPIFixture(t)
	token := fixture.login(t, "owner@example.test", "owner-test-password")
	response := fixture.do(t, http.MethodPost, "/v1/evidence", token, map[string]any{
		"kind":         domain.SourcePaper,
		"external_id":  "HTTP-CONFLICT-1",
		"title":        "Cotton evidence conflict example",
		"origin":       "Controlled repository",
		"abstract":     "A sufficiently detailed abstract used to exercise duplicate source conflict behavior through HTTP.",
		"fingerprint":  "duplicate-http-fingerprint",
		"content_hash": "duplicate-http-content",
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("first register status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = fixture.do(t, http.MethodPost, "/v1/evidence", token, map[string]any{
		"kind":         domain.SourcePaper,
		"external_id":  "HTTP-CONFLICT-2",
		"title":        "Different title with the same source fingerprint",
		"origin":       "Controlled repository",
		"abstract":     "Another sufficiently detailed abstract that should not bypass the source fingerprint uniqueness rule.",
		"fingerprint":  "duplicate-http-fingerprint",
		"content_hash": "different-content",
	})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var conflict errorEnvelope
	decodeResponse(t, response, &conflict)
	if conflict.Error.Code != "duplicate_source" || conflict.Error.RequestID == "" {
		t.Fatalf("conflict envelope=%#v", conflict)
	}
	response = fixture.do(t, http.MethodPost, "/v1/sessions/logout", token, map[string]any{})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = fixture.do(t, http.MethodGet, "/v1/evidence", token, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
}

func TestEvidenceRegistrationIdempotencyReplaysCommittedResponse(t *testing.T) {
	fixture := newAPIFixture(t)
	token := fixture.login(t, "owner@example.test", "owner-test-password")
	payload := map[string]any{
		"kind":         domain.SourcePatent,
		"external_id":  "CN-COTTON-IDEMPOTENT",
		"title":        "Cotton fiber processing patent evidence",
		"origin":       "National patent archive",
		"abstract":     "A detailed patent abstract used to verify that retries return the originally committed evidence unit.",
		"fingerprint":  "http-idempotent-fingerprint",
		"content_hash": "http-idempotent-content",
	}
	first := fixture.doWithKey(t, http.MethodPost, "/v1/evidence", token, "register-key-1", payload)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first idempotent request status=%d body=%s", first.StatusCode, readBody(t, first))
	}
	var firstResult evidence.RegisterResult
	decodeResponse(t, first, &firstResult)
	second := fixture.doWithKey(t, http.MethodPost, "/v1/evidence", token, "register-key-1", payload)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("replayed request status=%d body=%s", second.StatusCode, readBody(t, second))
	}
	var secondResult evidence.RegisterResult
	decodeResponse(t, second, &secondResult)
	if secondResult.Evidence.ID != firstResult.Evidence.ID || secondResult.Version.ID != firstResult.Version.ID {
		t.Fatalf("idempotent replay created a new result: first=%#v second=%#v", firstResult, secondResult)
	}
	changed := make(map[string]any, len(payload))
	for key, value := range payload {
		changed[key] = value
	}
	changed["abstract"] = "A changed request body must not reuse a committed idempotency key for another registration."
	third := fixture.doWithKey(t, http.MethodPost, "/v1/evidence", token, "register-key-1", changed)
	if third.StatusCode != http.StatusConflict {
		t.Fatalf("changed idempotent payload status=%d body=%s", third.StatusCode, readBody(t, third))
	}
	third.Body.Close()
	audits, err := fixture.store.AuditEvents(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	registrations := 0
	for _, event := range audits.Items {
		if event.Action == "evidence.registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatalf("idempotent replay wrote %d registration audits", registrations)
	}
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "http-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	identityService := identity.New(store, 2*time.Hour)
	if _, err := identityService.Bootstrap(context.Background(), "owner@example.test", "owner-test-password"); err != nil {
		store.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New(Dependencies{Identity: identityService, Evidence: evidence.New(store), Reviews: reviews.New(store),
		Publication: publication.New(store), Jobs: jobs.New(store), Store: store, Logger: logger, MaxBody: 1 << 20})
	server := httptest.NewServer(api.Handler())
	fixture := &apiFixture{server: server, store: store, client: server.Client()}
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return fixture
}

func (fixture *apiFixture) login(t *testing.T, email, password string) string {
	t.Helper()
	response := fixture.do(t, http.MethodPost, "/v1/sessions/login", "",
		map[string]any{"email": email, "password": password})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login %s status=%d body=%s", email, response.StatusCode, readBody(t, response))
	}
	var result identity.LoginResult
	decodeResponse(t, response, &result)
	if result.Token == "" {
		t.Fatalf("login %s returned empty token", email)
	}
	return result.Token
}

func (fixture *apiFixture) createUser(t *testing.T, token string, payload map[string]any) domain.User {
	t.Helper()
	response := fixture.do(t, http.MethodPost, "/v1/users", token, payload)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create user status=%d body=%s", response.StatusCode, readBody(t, response))
	}
	var user domain.User
	decodeResponse(t, response, &user)
	return user
}

func (fixture *apiFixture) do(t *testing.T, method, path, token string, payload any) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, fixture.server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "test-"+strings.ReplaceAll(path, "/", "-"))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func (fixture *apiFixture) doRaw(t *testing.T, method, path, token, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, fixture.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func (fixture *apiFixture) doWithKey(t *testing.T, method, path, token, idempotencyKey string, payload any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, fixture.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode HTTP %d response: %v", response.StatusCode, err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func number(value int64) string {
	return strconv.FormatInt(value, 10)
}
