package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// ErrSessionNotFound is returned when a session token is unknown or expired.
var ErrSessionNotFound = errors.New("auth: session not found")

// SessionCookieName is the name of the session cookie (ADR-003).
const SessionCookieName = "tierl_session"

// DefaultSessionTTL is how long a session stays valid without activity.
const DefaultSessionTTL = 30 * 24 * time.Hour

// Session is a server-side session. Only the SHA-256 of the cookie token is
// stored in the database, so a database leak does not expose usable tokens.
type Session struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Sessions persists server-side sessions.
type Sessions interface {
	// Create inserts a session. TokenHash is unique.
	Create(ctx context.Context, s *Session) error
	// GetByTokenHash returns a live session or ErrSessionNotFound.
	GetByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*Session, error)
	// Delete removes the session with the given token hash. Unknown sessions
	// are not an error.
	Delete(ctx context.Context, tokenHash string) error
	// DeleteByUserID removes every session of the user.
	DeleteByUserID(ctx context.Context, userID string) error
	// DeleteExpired removes sessions that expired at or before now.
	DeleteExpired(ctx context.Context, now time.Time) error
}

// SessionRevoker revokes sessions of a user. Both Sessions stores and
// SessionManager implement it.
type SessionRevoker interface {
	DeleteByUserID(ctx context.Context, userID string) error
}

// SessionOptions configure a SessionManager. Zero values select safe defaults.
type SessionOptions struct {
	// TTL is the session lifetime; the zero value means DefaultSessionTTL.
	TTL time.Duration
	// Now returns the current time; default time.Now.
	Now func() time.Time
	// Rand is the entropy source for session tokens; default crypto/rand.
	Rand io.Reader
}

// SessionManager creates, looks up and revokes sessions.
type SessionManager struct {
	sessions Sessions
	ttl      time.Duration
	now      func() time.Time
	rand     io.Reader
}

// NewSessionManager builds a SessionManager.
func NewSessionManager(sessions Sessions, opts SessionOptions) *SessionManager {
	if opts.TTL <= 0 {
		opts.TTL = DefaultSessionTTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	return &SessionManager{sessions: sessions, ttl: opts.TTL, now: opts.Now, rand: opts.Rand}
}

// NewSessionService is an alias for NewSessionManager.
func NewSessionService(sessions Sessions, opts SessionOptions) *SessionManager {
	return NewSessionManager(sessions, opts)
}

// TTL returns the configured session lifetime.
func (m *SessionManager) TTL() time.Duration { return m.ttl }

// Create opens a new session for userID and returns the raw cookie token. The
// caller is responsible for sending it to the client.
func (m *SessionManager) Create(ctx context.Context, userID string) (string, *Session, error) {
	token, err := newToken(m.rand, 32)
	if err != nil {
		return "", nil, err
	}
	now := m.now().UTC()
	s := &Session{
		TokenHash: HashToken(token),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(m.ttl),
	}
	if err := m.sessions.Create(ctx, s); err != nil {
		return "", nil, fmt.Errorf("auth: create session: %w", err)
	}
	return token, s, nil
}

// Lookup returns the live session for a raw cookie token.
func (m *SessionManager) Lookup(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrSessionNotFound
	}
	s, err := m.sessions.GetByTokenHash(ctx, HashToken(token), m.now().UTC())
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Delete closes the session with the given raw cookie token.
func (m *SessionManager) Delete(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := m.sessions.Delete(ctx, HashToken(token)); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

// DeleteByUserID closes every session of the user. It lets SessionManager
// satisfy SessionRevoker directly.
func (m *SessionManager) DeleteByUserID(ctx context.Context, userID string) error {
	return m.DeleteAll(ctx, userID)
}

// DeleteAll closes every session of the user.
func (m *SessionManager) DeleteAll(ctx context.Context, userID string) error {
	if err := m.sessions.DeleteByUserID(ctx, userID); err != nil {
		return fmt.Errorf("auth: delete user sessions: %w", err)
	}
	return nil
}

// DeleteExpired removes expired sessions.
func (m *SessionManager) DeleteExpired(ctx context.Context) error {
	if err := m.sessions.DeleteExpired(ctx, m.now().UTC()); err != nil {
		return fmt.Errorf("auth: delete expired sessions: %w", err)
	}
	return nil
}

// Authenticate is an alias for Lookup.
func (m *SessionManager) Authenticate(ctx context.Context, token string) (*Session, error) {
	return m.Lookup(ctx, token)
}

// Revoke is an alias for Delete.
func (m *SessionManager) Revoke(ctx context.Context, token string) error {
	return m.Delete(ctx, token)
}

// RevokeAll is an alias for DeleteAll.
func (m *SessionManager) RevokeAll(ctx context.Context, userID string) error {
	return m.DeleteAll(ctx, userID)
}

// HashToken returns the hex SHA-256 of a raw session token. The database only
// stores this value.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken(r io.Reader, n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRandom, err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CSRF validates stateless CSRF tokens. A token is a random nonce followed by
// an HMAC-SHA256 over it; validation needs only the server secret, so a token
// works for anonymous and authenticated requests alike.
type CSRF struct {
	secret []byte
	rand   io.Reader
}

var (
	defaultCSRFOnce   sync.Once
	defaultCSRFSecret []byte
)

func defaultCSRFKey() []byte {
	defaultCSRFOnce.Do(func() {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			panic("auth: crypto/rand is unavailable: " + err.Error())
		}
		defaultCSRFSecret = key
	})
	return defaultCSRFSecret
}

// NewCSRF builds a CSRF token source. An empty secret selects a random
// process-wide secret shared by every CSRF built without an explicit secret.
func NewCSRF(secret []byte) *CSRF {
	if len(secret) == 0 {
		secret = defaultCSRFKey()
	}
	return &CSRF{secret: append([]byte(nil), secret...), rand: rand.Reader}
}

// Token returns a fresh CSRF token.
func (c *CSRF) Token() (string, error) {
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRandom, err)
	}
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write(nonce)
	raw := append(nonce, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Valid reports whether token was issued by this (or the process-wide) CSRF
// source.
func (c *CSRF) Valid(token string) bool {
	if token == "" || len(token) > 1024 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32+sha256.Size {
		return false
	}
	nonce, sig := raw[:32], raw[32:]
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write(nonce)
	return subtle.ConstantTimeCompare(sig, mac.Sum(nil)) == 1
}
