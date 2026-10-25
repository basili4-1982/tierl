package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRevoker implements auth.SessionRevoker by deleting the server-side
// sessions of a user (ADR-003: a password change signs everywhere out).
//
// The sessions table arrives with the sessions module. Until that migration is
// applied the DELETE hits an undefined table and is treated as "nothing to
// revoke", so a password reset does not fail on a deployment that has not
// enabled sessions yet. Once the table exists the same call removes the rows.
type SessionRevoker struct {
	pool *pgxpool.Pool
}

// NewSessionRevoker wraps an existing pool. The caller owns the pool.
func NewSessionRevoker(pool *pgxpool.Pool) *SessionRevoker { return &SessionRevoker{pool: pool} }

// LogoutAll removes every session of the user and returns how many were
// removed.
func (s *SessionRevoker) LogoutAll(ctx context.Context, userID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if pe := pgErr(err); pe != nil && pe.Code == codeUndefinedTable {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: revoke sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
