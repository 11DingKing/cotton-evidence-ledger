package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"github.com/11DingKing/cotton-evidence-ledger/internal/storage"
)

type Service struct {
	store *storage.Store
	now   func() time.Time
}

type RegisterInput struct {
	Kind        domain.SourceType `json:"kind"`
	ExternalID  string            `json:"external_id"`
	Title       string            `json:"title"`
	Origin      string            `json:"origin"`
	Fingerprint string            `json:"fingerprint"`
	Abstract    string            `json:"abstract"`
	ContentHash string            `json:"content_hash"`
}

type RegisterResult struct {
	Evidence domain.Evidence        `json:"evidence"`
	Version  domain.EvidenceVersion `json:"version"`
}

func New(store *storage.Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = now
	return &copy
}

func (s *Service) Register(ctx context.Context, actor domain.Actor, input RegisterInput) (RegisterResult, error) {
	return s.register(ctx, actor, input, "")
}

func (s *Service) RegisterIdempotent(ctx context.Context, actor domain.Actor, input RegisterInput, idempotencyKey string) (RegisterResult, error) {
	idempotencyKey = strings.ToLower(idempotencyKey)
	if len(idempotencyKey) > 128 {
		return RegisterResult{}, apperr.New("invalid_idempotency_key", "幂等键不能超过 128 个字符", apperr.ErrInvalid)
	}
	return s.register(ctx, actor, input, idempotencyKey)
}

func (s *Service) register(ctx context.Context, actor domain.Actor, input RegisterInput, idempotencyKey string) (RegisterResult, error) {
	if !actor.Role.CanRegisterSource() {
		return RegisterResult{}, apperr.ErrForbidden
	}
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.Title = strings.TrimSpace(input.Title)
	input.Origin = strings.TrimSpace(input.Origin)
	input.Abstract = strings.TrimSpace(input.Abstract)
	input.Fingerprint = normalizeFingerprint(input.Fingerprint, input.Kind, input.ExternalID, input.Title)
	input.ContentHash = normalizeContentHash(input.ContentHash, input.Abstract)
	source := domain.Source{Kind: input.Kind, ExternalID: input.ExternalID, Title: input.Title,
		Origin: input.Origin, Fingerprint: input.Fingerprint, SubmitterID: actor.UserID}
	if err := domain.ValidateSource(source); err != nil {
		return RegisterResult{}, err
	}
	version := domain.EvidenceVersion{State: domain.VersionDraft, Title: input.Title,
		Abstract: input.Abstract, ContentHash: input.ContentHash, CreatedBy: actor.UserID}
	if err := domain.ValidateVersion(version); err != nil {
		return RegisterResult{}, err
	}
	if idempotencyKey == "" {
		if _, err := s.store.SourceByFingerprint(ctx, source.Fingerprint); err == nil {
			return RegisterResult{}, apperr.New("duplicate_source", "同一来源指纹已经登记", apperr.ErrConflict)
		} else if !errors.Is(err, apperr.ErrNotFound) {
			return RegisterResult{}, fmt.Errorf("check source fingerprint: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return RegisterResult{}, err
	}
	requestHash, err := registrationHash(actor.UserID, input)
	if err != nil {
		return RegisterResult{}, err
	}
	evidence, savedVersion, err := s.store.RegisterEvidence(ctx, storage.RegisterEvidenceParams{
		Source: source, Version: version, OwnerID: actor.UserID, RequestID: audit.RequestID(ctx),
		IdempotencyKey: idempotencyKey, RequestHash: requestHash, Now: s.now().UTC(),
	})
	if err != nil {
		return RegisterResult{}, fmt.Errorf("register evidence: %w", err)
	}
	return RegisterResult{Evidence: evidence, Version: savedVersion}, nil
}

func registrationHash(actorID int64, input RegisterInput) (string, error) {
	data, err := json.Marshal(struct {
		ActorID int64         `json:"actor_id"`
		Input   RegisterInput `json:"input"`
	}{ActorID: actorID, Input: input})
	if err != nil {
		return "", fmt.Errorf("encode registration idempotency input: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) AddClaim(ctx context.Context, actor domain.Actor, evidenceID, versionID, expectedRevision int64, claim domain.Claim) (domain.Claim, error) {
	if !actor.Role.CanExtractClaims() {
		return domain.Claim{}, apperr.ErrForbidden
	}
	claim.Statement = strings.TrimSpace(claim.Statement)
	claim.Locator = strings.TrimSpace(claim.Locator)
	if err := domain.ValidateClaim(claim); err != nil {
		return domain.Claim{}, err
	}
	saved, err := s.store.AddClaim(ctx, actor, evidenceID, versionID, claim, expectedRevision,
		audit.RequestID(ctx), s.now().UTC())
	if err != nil {
		return domain.Claim{}, fmt.Errorf("extract claim: %w", err)
	}
	return saved, nil
}

func (s *Service) Get(ctx context.Context, actor domain.Actor, evidenceID int64) (domain.Evidence, domain.EvidenceVersion, []domain.Claim, error) {
	evidence, err := s.store.EvidenceByID(ctx, evidenceID)
	if err != nil {
		return domain.Evidence{}, domain.EvidenceVersion{}, nil, err
	}
	if evidence.State != domain.EvidencePublished && evidence.OwnerID != actor.UserID && actor.Role == domain.RoleCollector {
		return domain.Evidence{}, domain.EvidenceVersion{}, nil, apperr.ErrForbidden
	}
	if evidence.CurrentVersionID == nil {
		return domain.Evidence{}, domain.EvidenceVersion{}, nil, apperr.ErrNotFound
	}
	version, err := s.store.VersionByID(ctx, *evidence.CurrentVersionID)
	if err != nil {
		return domain.Evidence{}, domain.EvidenceVersion{}, nil, err
	}
	claims, err := s.store.ClaimsByVersion(ctx, version.ID)
	if err != nil {
		return domain.Evidence{}, domain.EvidenceVersion{}, nil, err
	}
	return evidence, version, claims, nil
}

func (s *Service) List(ctx context.Context, actor domain.Actor, state domain.EvidenceState, ownerID, afterID int64, limit int) (domain.Page[domain.Evidence], error) {
	if state != "" && !state.Valid() {
		return domain.Page[domain.Evidence]{}, apperr.ErrInvalid
	}
	if actor.Role == domain.RoleCollector {
		ownerID = actor.UserID
	}
	return s.store.ListEvidence(ctx, state, ownerID, afterID, limit)
}

func normalizeFingerprint(value string, kind domain.SourceType, externalID, title string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value != "" {
		return value
	}
	sum := sha256.Sum256([]byte(strings.ToLower(string(kind) + "|" + externalID + "|" + title)))
	return hex.EncodeToString(sum[:])
}

func normalizeContentHash(value, abstract string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value != "" {
		return value
	}
	sum := sha256.Sum256([]byte(abstract))
	return hex.EncodeToString(sum[:])
}
