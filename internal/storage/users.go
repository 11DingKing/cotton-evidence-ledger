package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

type UserCredential struct {
	User         domain.User
	PasswordHash string
}

func (s *Store) CreateUser(ctx context.Context, email, name, passwordHash string, role domain.Role, now time.Time) (domain.User, error) {
	result, err := s.db.ExecContext(ctx, `
        INSERT INTO users(email, name, password_hash, role, active, created_at)
        VALUES(?,?,?,?,1,?)`, strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(name), passwordHash, role, formatTime(now))
	if err != nil {
		return domain.User{}, normalizeError("create user", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.User{}, fmt.Errorf("read user id: %w", err)
	}
	return s.UserByID(ctx, id)
}

func (s *Store) EnsureUser(ctx context.Context, email, name, passwordHash string, role domain.Role, now time.Time) (domain.User, error) {
	credential, err := s.CredentialByEmail(ctx, email)
	if err == nil {
		return credential.User, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, apperr.ErrNotFound) {
		return domain.User{}, fmt.Errorf("check bootstrap user: %w", err)
	}
	return s.CreateUser(ctx, email, name, passwordHash, role, now)
}

func (s *Store) UserByID(ctx context.Context, id int64) (domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
        SELECT id, email, name, role, active, created_at FROM users WHERE id = ?`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
        SELECT id, email, name, role, active, created_at FROM users WHERE email = ? COLLATE NOCASE`, strings.TrimSpace(email)))
}

func (s *Store) CredentialByEmail(ctx context.Context, email string) (UserCredential, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, email, name, role, active, created_at, password_hash
        FROM users WHERE email = ? COLLATE NOCASE`, strings.TrimSpace(email))
	var credential UserCredential
	var role string
	var active int
	var created string
	err := row.Scan(&credential.User.ID, &credential.User.Email, &credential.User.Name, &role, &active, &created, &credential.PasswordHash)
	if err != nil {
		return UserCredential{}, normalizeError("find credential", err)
	}
	credential.User.Role = domain.Role(role)
	credential.User.Active = active == 1
	credential.User.CreatedAt, err = parseTime(created)
	if err != nil {
		return UserCredential{}, err
	}
	return credential, nil
}

func (s *Store) SetUserActive(ctx context.Context, id int64, active bool) error {
	value := 0
	if active {
		value = 1
	}
	result, err := s.db.ExecContext(ctx, "UPDATE users SET active = ? WHERE id = ?", value, id)
	if err != nil {
		return normalizeError("set user active", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read active update count: %w", err)
	}
	if rows != 1 {
		return normalizeError("set user active", sql.ErrNoRows)
	}
	return nil
}

func scanUser(row *sql.Row) (domain.User, error) {
	var user domain.User
	var role string
	var active int
	var created string
	err := row.Scan(&user.ID, &user.Email, &user.Name, &role, &active, &created)
	if err != nil {
		return domain.User{}, normalizeError("scan user", err)
	}
	user.Role = domain.Role(role)
	user.Active = active == 1
	user.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.User{}, err
	}
	return user, nil
}
