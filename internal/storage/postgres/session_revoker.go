package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSessionsUnavailable means the sessions table does not exist. Revoking
// sessions after a password change cannot be faked: the caller must report the
// configuration problem instead of a successful reset.
var ErrSessionsUnavailable = errors.New("postgres: sessions table is missing; apply the sessions migration")

// SessionRevoker implements auth.SessionRevoker by deleting the server-side
// sessions of a user (ADR-003: a password change signs everywhere out).
//
// The sessions table arrives with the sessions module. Until that migration is
// applied every call fails loudly with ErrSessionsUnavailable, so a password
// reset is never reported as successful while old sessions stay alive.
type SessionRevoker struct {
	pool *pgxpool.Pool
}

// NewSessionRevoker wraps an existing pool. The caller owns the pool.
func NewSessionRevoker(pool *pgxpool.Pool) *SessionRevoker { return &SessionRevoker{pool: pool} }

// LogoutAll removes every session of the user and returns how many were
// removed. It returns ErrSessionsUnavailable when the table is absent.
func (s *SessionRevoker) LogoutAll(ctx context.Context, userID string) (int64, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('sessions') IS NOT NULL`).Scan(&exists); err != nil {
		return 0, fmt.Errorf("postgres: check sessions table: %w", err)
	}
	if !exists {
		return 0, ErrSessionsUnavailable
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("postgres: revoke sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
