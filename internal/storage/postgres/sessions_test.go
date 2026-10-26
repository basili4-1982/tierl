package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

func TestSessionStore(t *testing.T) {
	_, pool := newStore(t)
	ctx := context.Background()
	users := postgres.NewUserStore(pool)
	for _, id := range []string{"u1", "u2"} {
		if err := users.Create(ctx, user(id, id+"@example.com")); err != nil {
			t.Fatal(err)
		}
	}
	s := postgres.NewSessionStore(pool)
	mk := func(hash, userID string, ttl time.Duration) *auth.Session {
		return &auth.Session{TokenHash: hash, UserID: userID, CreatedAt: t0, ExpiresAt: t0.Add(ttl)}
	}
	live1, live2 := mk("hash1", "u1", time.Hour), mk("hash2", "u1", time.Hour)
	stale, other := mk("hash3", "u1", -time.Hour), mk("hash4", "u2", time.Hour)
	for _, sess := range []*auth.Session{live1, live2, stale, other} {
		if err := s.Create(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetByTokenHash(ctx, "hash1", t0)
	if err != nil || got.UserID != "u1" || !got.ExpiresAt.Equal(live1.ExpiresAt) {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := s.GetByTokenHash(ctx, "hash1", t0.Add(2*time.Hour)); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("expired session accepted: %v", err)
	}
	if _, err := s.GetByTokenHash(ctx, "missing", t0); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("missing session: %v", err)
	}

	if err := s.Delete(ctx, "hash1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByTokenHash(ctx, "hash1", t0); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteByUserID(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByTokenHash(ctx, "hash2", t0); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("delete by user: %v", err)
	}
	if _, err := s.GetByTokenHash(ctx, "hash4", t0); err != nil {
		t.Fatalf("unrelated user session removed: %v", err)
	}
	if err := s.DeleteExpired(ctx, t0); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("expired sweep: n=%d err=%v", n, err)
	}
}
