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

// Create implements auth.Sessions.
func (s *SessionStore) Create(ctx context.Context, sess *auth.Session) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4)`,
		sess.TokenHash, sess.UserID, sess.CreatedAt, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("postgres: create session: %w", err)
	}
	return nil
}

// GetByTokenHash implements auth.Sessions. Expired sessions are not returned.
func (s *SessionStore) GetByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*auth.Session, error) {
	var sess auth.Session
	err := s.pool.QueryRow(ctx, `
		SELECT token_hash, user_id, created_at, expires_at
		FROM sessions WHERE token_hash = $1 AND expires_at > $2`, tokenHash, now).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	return &sess, nil
}

// Delete implements auth.Sessions.
func (s *SessionStore) Delete(ctx context.Context, tokenHash string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("postgres: delete session: %w", err)
	}
	return nil
}

// DeleteByUserID implements auth.Sessions.
func (s *SessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("postgres: delete user sessions: %w", err)
	}
	return nil
}

// DeleteOthersByUserID implements auth.OtherSessionDeleter.
func (s *SessionStore) DeleteOthersByUserID(ctx context.Context, userID, keepTokenHash string) (int64, error) {
	if keepTokenHash == "" {
		tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
		if err != nil {
			return 0, fmt.Errorf("postgres: delete other sessions: %w", err)
		}
		return tag.RowsAffected(), nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, keepTokenHash)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete other sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpired implements auth.Sessions.
func (s *SessionStore) DeleteExpired(ctx context.Context, now time.Time) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now); err != nil {
		return fmt.Errorf("postgres: delete expired sessions: %w", err)
	}
	return nil
}

// Aliases kept for callers that prefer explicit session method names.

// CreateSession is an alias for Create.
func (s *SessionStore) CreateSession(ctx context.Context, sess *auth.Session) error {
	return s.Create(ctx, sess)
}

// GetSession is an alias for GetByTokenHash.
func (s *SessionStore) GetSession(ctx context.Context, tokenHash string, now time.Time) (*auth.Session, error) {
	return s.GetByTokenHash(ctx, tokenHash, now)
}

// DeleteSession is an alias for Delete.
func (s *SessionStore) DeleteSession(ctx context.Context, tokenHash string) error {
	return s.Delete(ctx, tokenHash)
}

// DeleteSessionsByUser is an alias for DeleteByUserID.
func (s *SessionStore) DeleteSessionsByUser(ctx context.Context, userID string) error {
	return s.DeleteByUserID(ctx, userID)
}

// DeleteExpiredSessions is an alias for DeleteExpired.
func (s *SessionStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return s.DeleteExpired(ctx, now)
}

// Session methods on Store let callers that already hold the tierlist store
// use the compact explicit names without constructing a SessionStore.

// CreateSession is an alias for SessionStore.Create.
func (s *Store) CreateSession(ctx context.Context, sess *auth.Session) error {
	return NewSessionStore(s.pool).Create(ctx, sess)
}

// GetSessionByTokenHash is an alias for SessionStore.GetByTokenHash.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*auth.Session, error) {
	return NewSessionStore(s.pool).GetByTokenHash(ctx, tokenHash, now)
}

// DeleteSession is an alias for SessionStore.Delete.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	return NewSessionStore(s.pool).Delete(ctx, tokenHash)
}

// DeleteUserSessions is an alias for SessionStore.DeleteByUserID.
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	return NewSessionStore(s.pool).DeleteByUserID(ctx, userID)
}

// DeleteExpiredSessions is an alias for SessionStore.DeleteExpired.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return NewSessionStore(s.pool).DeleteExpired(ctx, now)
}

// DeleteOtherUserSessions is an alias for SessionStore.DeleteOthersByUserID.
func (s *Store) DeleteOtherUserSessions(ctx context.Context, userID, keepTokenHash string) (int64, error) {
	return NewSessionStore(s.pool).DeleteOthersByUserID(ctx, userID, keepTokenHash)
}
