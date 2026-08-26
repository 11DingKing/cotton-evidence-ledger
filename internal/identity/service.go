package identity

import (
	"context"
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
	ttl   time.Duration
	now   func() time.Time
}

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      domain.User `json:"user"`
}

func New(store *storage.Store, ttl time.Duration) *Service {
	return &Service{store: store, ttl: ttl, now: time.Now}
}

func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = now
	return &copy
}

func (s *Service) Bootstrap(ctx context.Context, email, password string) (domain.User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return domain.User{}, err
	}
	user, err := s.store.EnsureUser(ctx, strings.ToLower(strings.TrimSpace(email)), "知识负责人", hash,
		domain.RoleKnowledgeOwner, s.now().UTC())
	if err != nil {
		return domain.User{}, fmt.Errorf("bootstrap knowledge owner: %w", err)
	}
	return user, nil
}

func (s *Service) CreateUser(ctx context.Context, actor domain.Actor, email, name, password string, role domain.Role) (domain.User, error) {
	if actor.Role != domain.RoleKnowledgeOwner {
		return domain.User{}, apperr.ErrForbidden
	}
	if !role.Valid() {
		return domain.User{}, apperr.New("invalid_role", "用户角色无效", apperr.ErrInvalid)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if !strings.Contains(email, "@") || len(name) < 2 {
		return domain.User{}, apperr.New("invalid_user", "邮箱或姓名无效", apperr.ErrInvalid)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return domain.User{}, apperr.New("invalid_password", "密码不符合长度要求", err)
	}
	user, err := s.store.CreateUser(ctx, email, name, hash, role, s.now().UTC())
	if err != nil {
		return domain.User{}, fmt.Errorf("create managed user: %w", err)
	}
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	credential, err := s.store.CredentialByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil || !credential.User.Active || !verifyPassword(credential.PasswordHash, password) {
		return LoginResult{}, apperr.New("invalid_credentials", "邮箱或密码不正确", apperr.ErrUnauthorized)
	}
	if err := ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	token, hash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	if _, err := s.store.CreateSessionAudited(ctx, credential.User.ID, hash, audit.RequestID(ctx), expiresAt, now); err != nil {
		return LoginResult{}, fmt.Errorf("persist login session: %w", err)
	}
	return LoginResult{Token: token, ExpiresAt: expiresAt, User: credential.User}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.Actor, error) {
	if strings.TrimSpace(token) == "" {
		return domain.Actor{}, apperr.ErrUnauthorized
	}
	actor, err := s.store.ActorByTokenHash(ctx, tokenHash(token), s.now().UTC())
	if err != nil {
		return domain.Actor{}, fmt.Errorf("authenticate bearer token: %w", err)
	}
	if !actor.Role.Valid() {
		return domain.Actor{}, apperr.ErrForbidden
	}
	return actor, nil
}

func (s *Service) Logout(ctx context.Context, actor domain.Actor) error {
	if err := s.store.RevokeSessionAudited(ctx, actor, audit.RequestID(ctx), s.now().UTC()); err != nil {
		return fmt.Errorf("logout session: %w", err)
	}
	return nil
}

func (s *Service) DisableUser(ctx context.Context, actor domain.Actor, userID int64) error {
	if actor.Role != domain.RoleKnowledgeOwner || actor.UserID == userID {
		return apperr.ErrForbidden
	}
	if err := s.store.SetUserActive(ctx, userID, false); err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	if _, err := s.store.RevokeAllUserSessions(ctx, userID, s.now().UTC()); err != nil {
		return fmt.Errorf("revoke disabled user sessions: %w", err)
	}
	return nil
}

func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	count, err := s.store.DeleteExpiredSessions(ctx, s.now().UTC().Add(-time.Nanosecond))
	if err != nil {
		return 0, fmt.Errorf("cleanup expired sessions: %w", err)
	}
	return count, nil
}
