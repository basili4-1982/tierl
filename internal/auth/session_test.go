package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memSessions struct {
	mu   sync.Mutex
	byID map[string]*Session
}

func newMemSessions() *memSessions { return &memSessions{byID: map[string]*Session{}} }

func (m *memSessions) Create(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[s.TokenHash]; ok {
		return errors.New("duplicate")
	}
	c := *s
	m.byID[s.TokenHash] = &c
	return nil
}

func (m *memSessions) GetByTokenHash(_ context.Context, hash string, now time.Time) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[hash]
	if !ok || !s.ExpiresAt.After(now) {
		return nil, ErrSessionNotFound
	}
	c := *s
	return &c, nil
}

func (m *memSessions) Delete(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, hash)
	return nil
}

func (m *memSessions) DeleteByUserID(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.byID {
		if s.UserID == userID {
			delete(m.byID, k)
		}
	}
	return nil
}

func (m *memSessions) DeleteExpired(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.byID {
		if !s.ExpiresAt.After(now) {
			delete(m.byID, k)
		}
	}
	return nil
}

func TestSessionManagerLifecycle(t *testing.T) {
	store := newMemSessions()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	m := NewSessionManager(store, SessionOptions{
		TTL:  time.Hour,
		Now:  func() time.Time { return now },
		Rand: strings.NewReader(strings.Repeat("a", 64)),
	})
	ctx := context.Background()

	token, sess, err := m.Create(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || sess.TokenHash == token || len(sess.TokenHash) != 64 {
		t.Fatalf("unexpected session: token=%q %+v", token, sess)
	}
	got, err := m.Lookup(ctx, token)
	if err != nil || got.UserID != "u1" {
		t.Fatalf("lookup: %v %+v", err, got)
	}
	if _, err := m.Lookup(ctx, "wrong"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	if err := m.Delete(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Lookup(ctx, token); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("deleted session still live: %v", err)
	}

	// Expired sessions are rejected and can be swept.
	tok2, _, _ := m.Create(ctx, "u2")
	now = now.Add(2 * time.Hour)
	if _, err := m.Lookup(ctx, tok2); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expired session accepted: %v", err)
	}
	if err := m.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.byID) != 0 {
		t.Fatalf("expired sessions not removed: %d", len(store.byID))
	}
}

func TestSessionManagerDeleteAll(t *testing.T) {
	store := newMemSessions()
	m := NewSessionManager(store, SessionOptions{})
	ctx := context.Background()
	t1, _, _ := m.Create(ctx, "u1")
	t2, _, _ := m.Create(ctx, "u1")
	t3, _, _ := m.Create(ctx, "u2")
	if err := m.DeleteByUserID(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{t1, t2} {
		if _, err := m.Lookup(ctx, tok); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("u1 session still live: %v", err)
		}
	}
	if _, err := m.Lookup(ctx, t3); err != nil {
		t.Fatalf("u2 session must survive: %v", err)
	}
}

func TestCSRFTokens(t *testing.T) {
	c := NewCSRF([]byte("test-secret"))
	tok, err := c.Token()
	if err != nil || tok == "" {
		t.Fatalf("token: %q %v", tok, err)
	}
	if !c.Valid(tok) {
		t.Fatal("fresh token must be valid")
	}
	if NewCSRF([]byte("test-secret")).Valid("garbage") {
		t.Fatal("garbage accepted")
	}
	if NewCSRF([]byte("other-secret")).Valid(tok) {
		t.Fatal("token accepted by a different secret")
	}
	// The process-wide default is shared, so independently built sources agree.
	a, b := NewCSRF(nil), NewCSRF(nil)
	tok2, _ := a.Token()
	if !b.Valid(tok2) {
		t.Fatal("default sources must share a secret")
	}
	c.rand = errReader{}
	if _, err := c.Token(); !errors.Is(err, ErrRandom) {
		t.Fatalf("want ErrRandom, got %v", err)
	}
}

func TestChangePasswordRevokesSessions(t *testing.T) {
	mem := newMem()
	sessions := newMemSessions()
	svc, err := NewService(mem, Options{
		Hash: fast, Sessions: sessions,
		Now: func() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, err := svc.Register(ctx, RegisterInput{Email: "anna@example.com", Password: goodPw, DisplayName: "Anna"})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewSessionManager(sessions, SessionOptions{Rand: strings.NewReader(strings.Repeat("c", 64))})
	token, _, err := manager.Create(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, u.ID, "wrong-password", "another-good-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong old password: %v", err)
	}
	if _, err := manager.Lookup(ctx, token); err != nil {
		t.Fatalf("failed change must keep sessions: %v", err)
	}
	if err := svc.ChangePassword(ctx, u.ID, goodPw, "another-good-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Lookup(ctx, token); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session survived password change: %v", err)
	}
	if _, err := svc.Login(ctx, "anna@example.com", "another-good-password"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
	if _, err := svc.Login(ctx, "anna@example.com", goodPw); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
	if err := svc.ChangePassword(ctx, u.ID, goodPw, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("weak password accepted: %v", err)
	}
}
