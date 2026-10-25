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

// SessionStore implements auth.Sessions.
type SessionStore struct {
	pool *pgxpool.Pool
}

var _ auth.Sessions = (*SessionStore)(nil)

// NewSessionStore wraps an existing pool. The caller owns the pool.
func NewSessionStore(pool *pgxpool.Pool) *SessionStore { return &SessionStore{pool: pool} }

const sessionColumns = `token_hash, user_id, csrf_secret, created_at, last_seen_at, expires_at, absolute_expires_at, revoked_at`

// Create implements auth.Sessions.
func (s *SessionStore) Create(ctx context.Context, sess *auth.Session) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (`+sessionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sess.TokenHash, sess.UserID, sess.CSRFSecret, sess.CreatedAt, sess.LastSeenAt,
		sess.ExpiresAt, sess.AbsoluteExpiresAt, sess.RevokedAt)
	if err != nil {
		return fmt.Errorf("postgres: create session: %w", err)
	}
	return nil
}

// Get implements auth.Sessions.
func (s *SessionStore) Get(ctx context.Context, tokenHash []byte) (*auth.Session, error) {
	var sess auth.Session
	err := s.pool.QueryRow(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE token_hash = $1`, tokenHash).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CSRFSecret, &sess.CreatedAt, &sess.LastSeenAt,
			&sess.ExpiresAt, &sess.AbsoluteExpiresAt, &sess.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	return &sess, nil
}

// Extend implements auth.Sessions.
func (s *SessionStore) Extend(ctx context.Context, tokenHash []byte, seenAt, expiresAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions
		SET last_seen_at = $2, expires_at = $3
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2 AND absolute_expires_at >= $3`,
		tokenHash, seenAt, expiresAt)
	if err != nil {
		return fmt.Errorf("postgres: extend session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}

// Revoke implements auth.Sessions.
func (s *SessionStore) Revoke(ctx context.Context, tokenHash []byte, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash, now)
	if err != nil {
		return fmt.Errorf("postgres: revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}

// RevokeAll implements auth.Sessions and auth.SessionRevoker.
func (s *SessionStore) RevokeAll(ctx context.Context, userID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL`,
		userID, now)
	if err != nil {
		return fmt.Errorf("postgres: revoke all sessions: %w", err)
	}
	return nil
}

// DeleteStale implements auth.Sessions. The two DELETEs are separate so the
// planner can use sessions_expires_at_idx for expired rows and the partial
// sessions_revoked_at_idx for revoked ones instead of a sequential scan.
// absolute_expires_at needs no separate predicate: expires_at is never later
// than it, so an absolutely expired row also matches expires_at <= now.
func (s *SessionStore) DeleteStale(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete expired sessions: %w", err)
	}
	total += tag.RowsAffected()

	tag, err = s.pool.Exec(ctx, `DELETE FROM sessions WHERE revoked_at IS NOT NULL`)
	if err != nil {
		return total, fmt.Errorf("postgres: delete revoked sessions: %w", err)
	}
	total += tag.RowsAffected()
	return total, nil
}
