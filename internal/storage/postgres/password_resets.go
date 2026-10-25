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

// PasswordResetStore implements auth.PasswordResets.
type PasswordResetStore struct {
	pool *pgxpool.Pool
}

var _ auth.PasswordResets = (*PasswordResetStore)(nil)

// NewPasswordResetStore wraps an existing pool. The caller owns the pool.
func NewPasswordResetStore(pool *pgxpool.Pool) *PasswordResetStore {
	return &PasswordResetStore{pool: pool}
}

// Create implements auth.PasswordResets.
func (s *PasswordResetStore) Create(ctx context.Context, t *auth.PasswordReset) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO password_reset_tokens (token_hash, user_id, created_at, expires_at, used_at)
		VALUES ($1, $2, $3, $4, $5)`,
		t.TokenHash, t.UserID, t.CreatedAt, t.ExpiresAt, t.UsedAt)
	if err != nil {
		return fmt.Errorf("postgres: create password reset token: %w", err)
	}
	return nil
}

// Consume implements auth.PasswordResets. The UPDATE ... RETURNING is a single
// statement, so two concurrent submissions of the same token cannot both win.
func (s *PasswordResetStore) Consume(ctx context.Context, tokenHash []byte, now time.Time) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx, `
		UPDATE password_reset_tokens SET used_at = $2
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING user_id`,
		tokenHash, now).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", auth.ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("postgres: consume password reset token: %w", err)
	}
	return userID, nil
}

// InvalidateAll implements auth.PasswordResets.
func (s *PasswordResetStore) InvalidateAll(ctx context.Context, userID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE password_reset_tokens SET used_at = $2
		WHERE user_id = $1 AND used_at IS NULL`,
		userID, now)
	if err != nil {
		return fmt.Errorf("postgres: invalidate password reset tokens: %w", err)
	}
	return nil
}
