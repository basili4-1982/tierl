// Package postgres implements the tierlist storage contracts on PostgreSQL
// using pgx v5.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/tierlist"
)

// PostgreSQL error codes and constraint names we translate to typed errors.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
	codeUndefinedTable      = "42P01"

	slugConstraint = "tierlists_slug_key"
)

// Store implements tierlist.Store and tierlist.Answers.
type Store struct {
	pool *pgxpool.Pool
}

var (
	_ tierlist.Store   = (*Store)(nil)
	_ tierlist.Answers = (*Store)(nil)
)

// New wraps an existing pool. The caller owns the pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Open connects to the database at url and verifies the connection.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}

// inTx runs fn in a transaction and commits it when fn succeeds.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		// Deferred constraints (unique positions) fail at commit.
		return mapWriteErr("commit", err)
	}
	return nil
}

// pgErr returns the PostgreSQL error inside err, if any.
func pgErr(err error) *pgconn.PgError {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe
	}
	return nil
}

// isSlugCollision reports whether err violates the unique slug constraint.
func isSlugCollision(err error) bool {
	pe := pgErr(err)
	return pe != nil && pe.Code == codeUniqueViolation && pe.ConstraintName == slugConstraint
}
