package auth_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/auth/authtest"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newSessions(t *testing.T, ttl, maxAge time.Duration) (*auth.SessionService, *authtest.Sessions, *testClock) {
	t.Helper()
	store := authtest.NewSessions()
	clock := newClock()
	svc := auth.NewSessionService(store, auth.SessionOptions{TTL: ttl, MaxAge: maxAge, Now: clock.Now})
	return svc, store, clock
}

func TestSessionCreateAuthenticate(t *testing.T) {
	svc, store, clock := newSessions(t, time.Hour, 24*time.Hour)
	ctx := context.Background()

	token, sess, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || sess.UserID != "u1" || len(sess.TokenHash) != 32 || len(sess.CSRFSecret) != 32 {
		t.Fatalf("session = %+v", sess)
	}
	if got := string(auth.HashSessionToken(token)); got != string(sess.TokenHash) {
		t.Error("token hash mismatch")
	}
	if !sess.ExpiresAt.Equal(clock.Now().Add(time.Hour)) || !sess.AbsoluteExpiresAt.Equal(clock.Now().Add(24*time.Hour)) {
		t.Errorf("times: %+v", sess)
	}
	if store.Len() != 1 {
		t.Errorf("stored = %d", store.Len())
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "u1" || string(got.CSRFSecret) != string(sess.CSRFSecret) {
		t.Fatalf("authenticated = %+v", got)
	}
}

func TestSessionSlidingRenewal(t *testing.T) {
	svc, store, clock := newSessions(t, time.Hour, 10*time.Hour)
	ctx := context.Background()
	token, created, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}

	// Soon after creation the expiry does not move (write throttling).
	clock.Advance(3 * time.Minute)
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(ctx, created.TokenHash)
	if !stored.ExpiresAt.Equal(created.ExpiresAt) || !stored.LastSeenAt.Equal(created.LastSeenAt) {
		t.Errorf("expiry moved too early: %+v", stored)
	}

	// After TTL/10 the idle window slides.
	clock.Advance(10 * time.Minute)
	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.After(created.ExpiresAt) || !got.LastSeenAt.Equal(clock.Now()) {
		t.Errorf("not extended: %+v", got)
	}
	stored, _ = store.Get(ctx, created.TokenHash)
	if !stored.ExpiresAt.Equal(got.ExpiresAt) || !stored.LastSeenAt.Equal(got.LastSeenAt) {
		t.Errorf("store not extended: %+v", stored)
	}
}

func TestSessionAbsoluteCap(t *testing.T) {
	svc, store, clock := newSessions(t, time.Hour, 3*time.Hour)
	ctx := context.Background()
	token, created, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}

	// Keep the session active until just before the absolute ceiling.
	for range 5 {
		clock.Advance(25 * time.Minute)
		if _, err := svc.Authenticate(ctx, token); err != nil {
			t.Fatalf("authenticate: %v", err)
		}
	}
	stored, _ := store.Get(ctx, created.TokenHash)
	if stored.ExpiresAt.After(created.AbsoluteExpiresAt) {
		t.Fatalf("expiry %v past absolute %v", stored.ExpiresAt, created.AbsoluteExpiresAt)
	}

	clock.Advance(2 * time.Hour)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("past absolute cap: %v", err)
	}
}

func TestSessionExpires(t *testing.T) {
	svc, _, clock := newSessions(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	token, _, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("expired: %v", err)
	}
}

func TestSessionLogoutAndLogoutAll(t *testing.T) {
	svc, _, _ := newSessions(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	first, _, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Logout(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, first); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("revoked session: %v", err)
	}
	// Unknown and malformed tokens are ignored by Logout.
	for _, token := range []string{"", "abc", strings.Repeat("a", 43)} {
		if err := svc.Logout(ctx, token); err != nil {
			t.Errorf("logout %q: %v", token, err)
		}
	}
	// Revoking an already revoked session is not an error for the caller.
	if err := svc.Logout(ctx, first); err != nil {
		t.Errorf("second logout: %v", err)
	}

	if _, err := svc.Authenticate(ctx, second); err != nil {
		t.Fatalf("second session: %v", err)
	}
	if err := svc.LogoutAll(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, second); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("logout all: %v", err)
	}
}

func TestSessionCleanup(t *testing.T) {
	svc, store, clock := newSessions(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	live, _, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	revoked, _, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(ctx, "u2"); err != nil {
		t.Fatal(err)
	}

	// A revoked session is removed while live ones stay.
	removed, err := svc.Cleanup(ctx, clock.Now())
	if err != nil || removed != 1 {
		t.Fatalf("cleanup revoked = %d, %v", removed, err)
	}
	if store.Len() != 2 {
		t.Fatalf("stored = %d, want 2", store.Len())
	}
	// After the idle TTL everything is stale.
	clock.Advance(2 * time.Hour)
	removed, err = svc.Cleanup(ctx, clock.Now())
	if err != nil || removed != 2 {
		t.Fatalf("cleanup expired = %d, %v", removed, err)
	}
	if store.Len() != 0 {
		t.Fatalf("stored = %d, want 0", store.Len())
	}
	if _, err := svc.Authenticate(ctx, live); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("live session after cleanup: %v", err)
	}
	// Storage errors are reported.
	store.Errs["DeleteStale"] = errors.New("db down")
	if _, err := svc.Cleanup(ctx, clock.Now()); err == nil {
		t.Fatal("expected cleanup error")
	}
}

func TestSessionInvalidTokens(t *testing.T) {
	svc, _, _ := newSessions(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	for _, token := range []string{"", "abc", strings.Repeat("a", 42), strings.Repeat("a", 44), "!!!!"} {
		if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Errorf("token %q: %v", token, err)
		}
	}
}

func TestSessionStorageErrors(t *testing.T) {
	svc, store, _ := newSessions(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	token, _, err := svc.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}

	boom := errors.New("db down")
	store.Errs["Get"] = boom
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, boom) {
		t.Errorf("get error = %v", err)
	}
	delete(store.Errs, "Get")

	store.Errs["Create"] = boom
	if _, _, err := svc.Create(ctx, "u2"); !errors.Is(err, boom) {
		t.Errorf("create error = %v", err)
	}

	store.Errs["RevokeAll"] = boom
	if err := svc.LogoutAll(ctx, "u1"); !errors.Is(err, boom) {
		t.Errorf("logout all error = %v", err)
	}
	if err := svc.RevokeAll(ctx, "u1", time.Now()); !errors.Is(err, boom) {
		t.Errorf("revoke all error = %v", err)
	}
}

func TestSessionRandomFailure(t *testing.T) {
	store := authtest.NewSessions()
	svc := auth.NewSessionService(store, auth.SessionOptions{Rand: failingReader{}})
	if _, _, err := svc.Create(context.Background(), "u1"); !errors.Is(err, auth.ErrRandom) {
		t.Fatalf("random failure = %v", err)
	}
	if store.Len() != 0 {
		t.Errorf("session stored despite failure")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
