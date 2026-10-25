// Package authtest provides in-memory fakes of the auth storage contracts for
// unit tests of the session, reset and HTTP layers.
package authtest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
)

// Users is a map-backed auth.Users.
type Users struct {
	mu   sync.Mutex
	byID map[string]*auth.User

	// Errs makes the named method ("Create", "GetByEmail", "GetByID",
	// "UpdatePasswordHash") fail with the error.
	Errs map[string]error
}

var _ auth.Users = (*Users)(nil)

// NewUsers returns an empty user directory.
func NewUsers() *Users { return &Users{byID: map[string]*auth.User{}, Errs: map[string]error{}} }

// Put stores a copy of u directly.
func (m *Users) Put(u *auth.User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *u
	m.byID[u.ID] = &c
}

// Len returns the number of users.
func (m *Users) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}

// Create implements auth.Users.
func (m *Users) Create(_ context.Context, u *auth.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Create"]; err != nil {
		return err
	}
	for _, existing := range m.byID {
		if existing.Email == u.Email {
			return auth.ErrEmailTaken
		}
	}
	c := *u
	m.byID[u.ID] = &c
	return nil
}

// GetByEmail implements auth.Users.
func (m *Users) GetByEmail(_ context.Context, email string) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["GetByEmail"]; err != nil {
		return nil, err
	}
	for _, u := range m.byID {
		if u.Email == email {
			c := *u
			return &c, nil
		}
	}
	return nil, auth.ErrUserNotFound
}

// GetByID implements auth.Users.
func (m *Users) GetByID(_ context.Context, id string) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["GetByID"]; err != nil {
		return nil, err
	}
	u, ok := m.byID[id]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	c := *u
	return &c, nil
}

// UpdatePasswordHash implements auth.Users.
func (m *Users) UpdatePasswordHash(_ context.Context, id, hash string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["UpdatePasswordHash"]; err != nil {
		return err
	}
	u, ok := m.byID[id]
	if !ok {
		return auth.ErrUserNotFound
	}
	u.PasswordHash, u.UpdatedAt = hash, now
	return nil
}

// Sessions is a map-backed auth.Sessions.
type Sessions struct {
	mu     sync.Mutex
	byHash map[string]*auth.Session
	gets   int

	// Errs makes the named method fail with the error.
	Errs map[string]error
}

var _ auth.Sessions = (*Sessions)(nil)

// NewSessions returns an empty session store.
func NewSessions() *Sessions {
	return &Sessions{byHash: map[string]*auth.Session{}, Errs: map[string]error{}}
}

func cloneSession(s *auth.Session) *auth.Session {
	c := *s
	c.TokenHash = slices.Clone(s.TokenHash)
	c.CSRFSecret = slices.Clone(s.CSRFSecret)
	return &c
}

// Len returns the number of stored sessions.
func (m *Sessions) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byHash)
}

// GetCalls returns how many times Get was called; tests use it to prove that
// one request does not read the session twice.
func (m *Sessions) GetCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gets
}

// Create implements auth.Sessions.
func (m *Sessions) Create(_ context.Context, s *auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Create"]; err != nil {
		return err
	}
	m.byHash[string(s.TokenHash)] = cloneSession(s)
	return nil
}

// Get implements auth.Sessions.
func (m *Sessions) Get(_ context.Context, tokenHash []byte) (*auth.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if err := m.Errs["Get"]; err != nil {
		return nil, err
	}
	s, ok := m.byHash[string(tokenHash)]
	if !ok {
		return nil, auth.ErrSessionNotFound
	}
	return cloneSession(s), nil
}

// Extend implements auth.Sessions.
func (m *Sessions) Extend(_ context.Context, tokenHash []byte, seenAt, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Extend"]; err != nil {
		return err
	}
	s, ok := m.byHash[string(tokenHash)]
	if !ok || s.RevokedAt != nil || !seenAt.Before(s.ExpiresAt) {
		return auth.ErrSessionNotFound
	}
	s.LastSeenAt, s.ExpiresAt = seenAt, expiresAt
	return nil
}

// Revoke implements auth.Sessions.
func (m *Sessions) Revoke(_ context.Context, tokenHash []byte, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Revoke"]; err != nil {
		return err
	}
	s, ok := m.byHash[string(tokenHash)]
	if !ok || s.RevokedAt != nil {
		return auth.ErrSessionNotFound
	}
	s.RevokedAt = &now
	return nil
}

// RevokeAll implements auth.Sessions.
func (m *Sessions) RevokeAll(_ context.Context, userID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["RevokeAll"]; err != nil {
		return err
	}
	for _, s := range m.byHash {
		if s.UserID == userID && s.RevokedAt == nil {
			t := now
			s.RevokedAt = &t
		}
	}
	return nil
}

// DeleteStale implements auth.Sessions.
func (m *Sessions) DeleteStale(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["DeleteStale"]; err != nil {
		return 0, err
	}
	var removed int64
	for hash, s := range m.byHash {
		if !now.Before(s.ExpiresAt) || !now.Before(s.AbsoluteExpiresAt) || s.RevokedAt != nil {
			delete(m.byHash, hash)
			removed++
		}
	}
	return removed, nil
}

// Resets is a map-backed auth.PasswordResets.
type Resets struct {
	mu     sync.Mutex
	byHash map[string]*auth.PasswordReset

	// Errs makes the named method fail with the error.
	Errs map[string]error
}

var _ auth.PasswordResets = (*Resets)(nil)

// NewResets returns an empty reset token store.
func NewResets() *Resets {
	return &Resets{byHash: map[string]*auth.PasswordReset{}, Errs: map[string]error{}}
}

// Len returns the number of stored tokens.
func (m *Resets) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byHash)
}

// Create implements auth.PasswordResets.
func (m *Resets) Create(_ context.Context, t *auth.PasswordReset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Create"]; err != nil {
		return err
	}
	c := *t
	m.byHash[string(t.TokenHash)] = &c
	return nil
}

// Consume implements auth.PasswordResets.
func (m *Resets) Consume(_ context.Context, tokenHash []byte, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Consume"]; err != nil {
		return "", err
	}
	t, ok := m.byHash[string(tokenHash)]
	if !ok || t.UsedAt != nil || !now.Before(t.ExpiresAt) {
		return "", auth.ErrInvalidToken
	}
	at := now
	t.UsedAt = &at
	return t.UserID, nil
}

// InvalidateAll implements auth.PasswordResets.
func (m *Resets) InvalidateAll(_ context.Context, userID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["InvalidateAll"]; err != nil {
		return err
	}
	for _, t := range m.byHash {
		if t.UserID == userID && t.UsedAt == nil {
			at := now
			t.UsedAt = &at
		}
	}
	return nil
}

// Mail is one message captured by Mailer.
type Mail struct {
	To    string
	Token string
}

// Mailer captures reset links instead of sending them.
type Mailer struct {
	mu   sync.Mutex
	Sent []Mail
	Err  error
}

var _ auth.Mailer = (*Mailer)(nil)

// SendPasswordReset implements auth.Mailer.
func (m *Mailer) SendPasswordReset(_ context.Context, email, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.Sent = append(m.Sent, Mail{To: email, Token: token})
	return nil
}

// Last returns the most recent message.
func (m *Mailer) Last() (Mail, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Sent) == 0 {
		return Mail{}, false
	}
	return m.Sent[len(m.Sent)-1], true
}
