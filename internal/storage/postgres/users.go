package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/auth"
)

const emailConstraint = "users_email_key"

// UserStore implements auth.Users.
type UserStore struct {
	pool *pgxpool.Pool
}

var _ auth.Users = (*UserStore)(nil)

// NewUserStore wraps an existing pool. The caller owns the pool.
func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

const userColumns = `id, email, display_name, password_hash, email_verified, created_at, updated_at`

// Create implements auth.Users.
func (s *UserStore) Create(ctx context.Context, u *auth.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (`+userColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		u.ID, u.Email, u.DisplayName, u.PasswordHash, u.EmailVerified, u.CreatedAt, u.UpdatedAt)
	if pe := pgErr(err); pe != nil && pe.Code == codeUniqueViolation && pe.ConstraintName == emailConstraint {
		return auth.ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("postgres: create user: %w", err)
	}
	return nil
}

// GetByEmail implements auth.Users.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*auth.User, error) {
	return s.get(ctx, "email", email)
}

// GetByID implements auth.Users.
func (s *UserStore) GetByID(ctx context.Context, id string) (*auth.User, error) {
	return s.get(ctx, "id", id)
}

// get loads a user by a column that is "id" or "email" (never user input).
func (s *UserStore) get(ctx context.Context, column, value string) (*auth.User, error) {
	var u auth.User
	err := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE `+column+` = $1`, value).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.EmailVerified, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get user: %w", err)
	}
	return &u, nil
}

// UpdatePasswordHash implements auth.Users.
func (s *UserStore) UpdatePasswordHash(ctx context.Context, id, hash string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = GREATEST($3, created_at) WHERE id = $1`, id, hash, now)
	if err != nil {
		return fmt.Errorf("postgres: update password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}

// UpdatePasswordAndDeleteSessions implements auth.PasswordSessions: the hash
// update and the removal of every other session of the user commit together.
func (s *UserStore) UpdatePasswordAndDeleteSessions(ctx context.Context, id, hash, keepTokenHash string, now time.Time) (removed int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("postgres: begin password change: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = GREATEST($3, created_at) WHERE id = $1`, id, hash, now)
	if err != nil {
		return 0, fmt.Errorf("postgres: update password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, auth.ErrUserNotFound
	}
	if keepTokenHash == "" {
		tag, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
	} else {
		tag, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, id, keepTokenHash)
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: delete other sessions: %w", err)
	}
	removed = tag.RowsAffected()
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("postgres: commit password change: %w", err)
	}
	return removed, nil
}

// MarkEmailVerified implements auth.EmailMarker.
func (s *UserStore) MarkEmailVerified(ctx context.Context, id string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET email_verified = true, updated_at = GREATEST($2, created_at) WHERE id = $1`, id, now)
	if err != nil {
		return fmt.Errorf("postgres: mark email verified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}
