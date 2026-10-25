package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

func TestSessionRevokerWithoutSessionsTable(t *testing.T) {
	// The sessions migration belongs to another branch. A reset must fail
	// loudly instead of claiming success while old sessions stay alive.
	_, pool := newStore(t)
	n, err := postgres.NewSessionRevoker(pool).LogoutAll(context.Background(), "u1")
	if n != 0 || !errors.Is(err, postgres.ErrSessionsUnavailable) {
		t.Fatalf("LogoutAll = %d, %v; want ErrSessionsUnavailable", n, err)
	}
}

func TestSessionRevokerDeletesUserSessions(t *testing.T) {
	_, pool := newStore(t)
	ctx := context.Background()
	// Minimal stand-in for the sessions table owned by the sessions module.
	if _, err := pool.Exec(ctx, `CREATE TABLE sessions (user_id text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS sessions`) }()
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (user_id) VALUES ('u1'), ('u1'), ('u2')`); err != nil {
		t.Fatal(err)
	}

	n, err := postgres.NewSessionRevoker(pool).LogoutAll(ctx, "u1")
	if err != nil || n != 2 {
		t.Fatalf("LogoutAll = %d, %v; want 2", n, err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = 'u2'`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("other user's session: %d %v", left, err)
	}
}

func TestSessionRevokerPropagatesQueryError(t *testing.T) {
	_, pool := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := postgres.NewSessionRevoker(pool).LogoutAll(ctx, "u1"); err == nil {
		t.Fatal("a database error must be reported")
	}
}

func TestSessionRevokerPropagatesDeleteError(t *testing.T) {
	_, pool := newStore(t)
	ctx := context.Background()
	// A view satisfies to_regclass but cannot be deleted from, which exercises
	// the DELETE error path without revoking table privileges.
	if _, err := pool.Exec(ctx, `CREATE VIEW sessions AS SELECT 'u1'::text AS user_id`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DROP VIEW IF EXISTS sessions`) }()
	if _, err := postgres.NewSessionRevoker(pool).LogoutAll(ctx, "u1"); err == nil {
		t.Fatal("a failed DELETE must be reported")
	}
}
