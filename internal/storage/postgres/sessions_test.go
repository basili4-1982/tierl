package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

func newAuthStores(t *testing.T) (*postgres.UserStore, *postgres.SessionStore, *postgres.PasswordResetStore, *pgxpool.Pool) {
	t.Helper()
	_, pool := newStore(t)
	return postgres.NewUserStore(pool), postgres.NewSessionStore(pool), postgres.NewPasswordResetStore(pool), pool
}

func sessionFor(t *testing.T, token byte, userID string, now time.Time) *auth.Session {
	t.Helper()
	hash := make([]byte, 32)
	hash[0] = token
	secret := make([]byte, 32)
	secret[0] = token
	return &auth.Session{
		TokenHash:         hash,
		UserID:            userID,
		CSRFSecret:        secret,
		CreatedAt:         now,
		LastSeenAt:        now,
		ExpiresAt:         now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}
}

func TestSessionsStore(t *testing.T) {
	users, sessions, _, _ := newAuthStores(t)
	ctx := context.Background()
	if err := users.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}

	sess := sessionFor(t, 1, "u1", t0)
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, err := sessions.Get(ctx, sess.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "u1" || len(got.CSRFSecret) != 32 || !got.CreatedAt.Equal(t0) ||
		!got.ExpiresAt.Equal(sess.ExpiresAt) || got.RevokedAt != nil {
		t.Fatalf("session = %+v", got)
	}
	if _, err := sessions.Get(ctx, []byte("missing")); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("missing = %v", err)
	}

	// Extend moves the timestamps.
	seen, expires := t0.Add(10*time.Minute), t0.Add(70*time.Minute)
	if err := sessions.Extend(ctx, sess.TokenHash, seen, expires); err != nil {
		t.Fatal(err)
	}
	got, _ = sessions.Get(ctx, sess.TokenHash)
	if !got.LastSeenAt.Equal(seen) || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("extended = %+v", got)
	}
	// An expired row cannot be extended.
	if err := sessions.Extend(ctx, sess.TokenHash, expires, expires.Add(time.Hour)); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("extend expired = %v", err)
	}

	// Revoke is idempotent for the caller but typed for the store.
	if err := sessions.Revoke(ctx, sess.TokenHash, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Revoke(ctx, sess.TokenHash, t0.Add(time.Minute)); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("second revoke = %v", err)
	}
	got, _ = sessions.Get(ctx, sess.TokenHash)
	if got.RevokedAt == nil {
		t.Fatal("revoked_at is nil")
	}
	if err := sessions.Extend(ctx, sess.TokenHash, seen, expires); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("extend revoked = %v", err)
	}
	// RevokeAll is a no-op when nothing is live.
	if err := sessions.RevokeAll(ctx, "u1", t0); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsStoreRevokeAll(t *testing.T) {
	users, sessions, _, _ := newAuthStores(t)
	ctx := context.Background()
	for _, id := range []string{"u1", "u2"} {
		if err := users.Create(ctx, user(id, id+"@example.com")); err != nil {
			t.Fatal(err)
		}
	}
	first := sessionFor(t, 1, "u1", t0)
	second := sessionFor(t, 2, "u1", t0)
	other := sessionFor(t, 3, "u2", t0)
	for _, s := range []*auth.Session{first, second, other} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := sessions.RevokeAll(ctx, "u1", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*auth.Session{first, second} {
		got, _ := sessions.Get(ctx, s.TokenHash)
		if got.RevokedAt == nil {
			t.Errorf("%s not revoked", s.UserID)
		}
	}
	if got, _ := sessions.Get(ctx, other.TokenHash); got.RevokedAt != nil {
		t.Error("session of another user was revoked")
	}
}

func TestSessionsConstraints(t *testing.T) {
	users, sessions, _, pool := newAuthStores(t)
	ctx := context.Background()
	if err := users.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(ctx, sessionFor(t, 1, "missing", t0)); err == nil {
		t.Error("unknown user must violate the foreign key")
	}
	bad := sessionFor(t, 2, "u1", t0)
	bad.ExpiresAt = t0.Add(-time.Minute)
	if err := sessions.Create(ctx, bad); err == nil {
		t.Error("expiry before creation must be rejected")
	}
	short := sessionFor(t, 3, "u1", t0)
	short.CSRFSecret = []byte("short")
	if err := sessions.Create(ctx, short); err == nil {
		t.Error("short csrf secret must be rejected")
	}
	dup := sessionFor(t, 6, "u1", t0)
	if err := sessions.Create(ctx, dup); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(ctx, sessionFor(t, 6, "u1", t0)); err == nil {
		t.Error("duplicate token hash must be rejected")
	}
	// Deleting the user removes the sessions.
	live := sessionFor(t, 5, "u1", t0)
	if err := sessions.Create(ctx, live); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Get(ctx, live.TokenHash); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("get after delete = %v", err)
	}
}

func TestPasswordResetStore(t *testing.T) {
	users, _, resets, pool := newAuthStores(t)
	ctx := context.Background()
	if err := users.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	hash := make([]byte, 32)
	hash[0] = 7
	token := &auth.PasswordReset{TokenHash: hash, UserID: "u1", CreatedAt: t0, ExpiresAt: t0.Add(30 * time.Minute)}
	if err := resets.Create(ctx, token); err != nil {
		t.Fatal(err)
	}

	userID, err := resets.Consume(ctx, hash, t0.Add(time.Minute))
	if err != nil || userID != "u1" {
		t.Fatalf("consume = %q %v", userID, err)
	}
	// Second use and expired tokens are rejected.
	if _, err := resets.Consume(ctx, hash, t0.Add(2*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("reuse = %v", err)
	}
	var used *time.Time
	if err := pool.QueryRow(ctx, `SELECT used_at FROM password_reset_tokens WHERE token_hash = $1`, hash).Scan(&used); err != nil || used == nil {
		t.Fatalf("used_at = %v %v", used, err)
	}

	expired := make([]byte, 32)
	expired[0] = 8
	if err := resets.Create(ctx, &auth.PasswordReset{
		TokenHash: expired, UserID: "u1", CreatedAt: t0, ExpiresAt: t0.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := resets.Consume(ctx, expired, t0.Add(2*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("expired = %v", err)
	}

	// InvalidateAll kills live tokens of one user only.
	var tokens [][]byte
	for i := range 2 {
		h := make([]byte, 32)
		h[0] = byte(20 + i)
		tokens = append(tokens, h)
		if err := resets.Create(ctx, &auth.PasswordReset{
			TokenHash: h, UserID: "u1", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	other := make([]byte, 32)
	other[0] = 30
	if err := users.Create(ctx, user("u2", "bob@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := resets.Create(ctx, &auth.PasswordReset{
		TokenHash: other, UserID: "u2", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := resets.InvalidateAll(ctx, "u1", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, h := range tokens {
		if _, err := resets.Consume(ctx, h, t0.Add(2*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("token %v not invalidated: %v", h[0], err)
		}
	}
	if _, err := resets.Consume(ctx, other, t0.Add(2*time.Minute)); err != nil {
		t.Errorf("token of another user invalidated: %v", err)
	}

	// The foreign key and timestamps are enforced.
	bad := make([]byte, 32)
	bad[0] = 40
	if err := resets.Create(ctx, &auth.PasswordReset{TokenHash: bad, UserID: "missing", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err == nil {
		t.Error("unknown user must violate the foreign key")
	}
	if err := resets.Create(ctx, &auth.PasswordReset{TokenHash: bad, UserID: "u1", CreatedAt: t0, ExpiresAt: t0}); err == nil {
		t.Error("non-positive lifetime must be rejected")
	}
}

func TestPasswordResetConcurrentConsume(t *testing.T) {
	users, _, resets, _ := newAuthStores(t)
	ctx := context.Background()
	if err := users.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	hash := make([]byte, 32)
	hash[0] = 9
	if err := resets.Create(ctx, &auth.PasswordReset{
		TokenHash: hash, UserID: "u1", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = resets.Consume(ctx, hash, t0.Add(time.Minute))
		}()
	}
	wg.Wait()
	var ok, invalid int
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, auth.ErrInvalidToken):
			invalid++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || invalid != n-1 {
		t.Fatalf("ok=%d invalid=%d", ok, invalid)
	}
}
