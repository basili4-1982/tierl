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

// TokenStore implements auth.TokenStore.
type TokenStore struct {
	pool *pgxpool.Pool
}

var _ auth.TokenStore = (*TokenStore)(nil)

// NewTokenStore wraps an existing pool. The caller owns the pool.
func NewTokenStore(pool *pgxpool.Pool) *TokenStore { return &TokenStore{pool: pool} }

const oneTimeColumns = `id, user_id, purpose, token_hash, expires_at, used_at, created_at`

// Issue implements auth.TokenStore. In one transaction it marks the previous
// active tokens of the same user and purpose used and inserts the new token.
func (s *TokenStore) Issue(ctx context.Context, t *auth.OneTimeToken) error {
	now := t.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin issue token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize issuance per user: two concurrent resends must not leave two
	// active tokens. The partial unique index is the second line of defence.
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, t.UserID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrUserNotFound
		}
		return fmt.Errorf("postgres: lock user for token: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE one_time_tokens SET used_at = $3
		WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`,
		t.UserID, t.Purpose, now); err != nil {
		return fmt.Errorf("postgres: invalidate previous tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO one_time_tokens (`+oneTimeColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, t.UserID, t.Purpose, t.TokenHash, t.ExpiresAt, t.UsedAt, t.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit token: %w", err)
	}
	return nil
}

// Consume implements auth.TokenStore. The UPDATE ... RETURNING is atomic, so
// two concurrent redemptions of the same token produce exactly one success.
func (s *TokenStore) Consume(ctx context.Context, tokenHash []byte, purpose auth.Purpose, now time.Time) (*auth.OneTimeToken, error) {
	var (
		t           auth.OneTimeToken
		purposeText string
	)
	err := s.pool.QueryRow(ctx, `
		UPDATE one_time_tokens SET used_at = $3
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3
		RETURNING `+oneTimeColumns,
		tokenHash, string(purpose), now).
		Scan(&t.ID, &t.UserID, &purposeText, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: consume token: %w", err)
	}
	t.Purpose = auth.Purpose(purposeText)
	return &t, nil
}

// Invalidate implements auth.TokenStore.
func (s *TokenStore) Invalidate(ctx context.Context, userID string, purpose auth.Purpose, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE one_time_tokens SET used_at = $3
		WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`,
		userID, string(purpose), now)
	if err != nil {
		return fmt.Errorf("postgres: invalidate tokens: %w", err)
	}
	return nil
}
