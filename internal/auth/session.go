package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	// SessionCookieName is the cookie that carries the session token
	// (api/openapi.yaml, security scheme sessionCookie).
	SessionCookieName = "tierl_session"

	// DefaultSessionTTL is the sliding idle lifetime of a session: every
	// authenticated request pushes it forward.
	DefaultSessionTTL = 30 * 24 * time.Hour
	// DefaultSessionMaxAge is the absolute lifetime of a session. Sliding
	// renewal never extends a session past it.
	DefaultSessionMaxAge = 90 * 24 * time.Hour

	sessionTokenBytes = 32
	csrfSecretBytes   = 32
	// sessionRefreshDivisor throttles sliding writes: last_seen_at and
	// expires_at are updated at most once per TTL/10.
	sessionRefreshDivisor = 10
)

// Session is a server-side login session. Only the SHA-256 of the cookie token
// is stored, so a database leak does not expose working tokens (ADR-003).
type Session struct {
	TokenHash         []byte
	UserID            string
	CSRFSecret        []byte
	CreatedAt         time.Time
	LastSeenAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

// live reports whether the session may still authenticate a request.
func (s *Session) live(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}

// Sessions persists server-side sessions.
type Sessions interface {
	// Create inserts a session.
	Create(ctx context.Context, s *Session) error
	// Get returns the session with the given token hash or ErrSessionNotFound.
	// Revoked and expired rows are returned too; the caller decides.
	Get(ctx context.Context, tokenHash []byte) (*Session, error)
	// Extend updates last_seen_at and expires_at of a live session or returns
	// ErrSessionNotFound.
	Extend(ctx context.Context, tokenHash []byte, seenAt, expiresAt time.Time) error
	// Revoke marks one session revoked; an unknown or already revoked session
	// yields ErrSessionNotFound.
	Revoke(ctx context.Context, tokenHash []byte, now time.Time) error
	// RevokeAll revokes every live session of the user.
	RevokeAll(ctx context.Context, userID string, now time.Time) error
	// DeleteStale removes expired and revoked sessions and returns how many
	// rows were deleted. It is the periodic cleanup used by SessionService.
	DeleteStale(ctx context.Context, now time.Time) (int64, error)
}

// SessionRevoker is the part of the session storage password reset needs.
type SessionRevoker interface {
	RevokeAll(ctx context.Context, userID string, now time.Time) error
}

// SessionOptions configure a SessionService. Zero values select safe defaults.
type SessionOptions struct {
	// TTL is the sliding idle lifetime; default DefaultSessionTTL.
	TTL time.Duration
	// MaxAge is the absolute lifetime; default DefaultSessionMaxAge and never
	// less than TTL.
	MaxAge time.Duration
	// Now returns the current time; default time.Now.
	Now func() time.Time
	// Rand is the entropy source; default crypto/rand.
	Rand io.Reader
}

// SessionService creates, validates and revokes sessions.
type SessionService struct {
	store  Sessions
	ttl    time.Duration
	maxAge time.Duration
	now    func() time.Time
	rand   io.Reader
}

// NewSessionService builds a SessionService.
func NewSessionService(store Sessions, opts SessionOptions) *SessionService {
	if opts.TTL <= 0 {
		opts.TTL = DefaultSessionTTL
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = DefaultSessionMaxAge
	}
	if opts.MaxAge < opts.TTL {
		opts.MaxAge = opts.TTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	return &SessionService{store: store, ttl: opts.TTL, maxAge: opts.MaxAge, now: opts.Now, rand: opts.Rand}
}

// HashSessionToken returns the SHA-256 of a session token, the value stored in
// the database.
func HashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// newRandomToken returns n random bytes encoded as unpadded base64url.
func newRandomToken(r io.Reader, n int) (string, error) {
	b, err := newRandomBytes(r, n)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newRandomBytes returns n random bytes.
func newRandomBytes(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRandom, err)
	}
	return b, nil
}

// validToken reports whether token has the exact shape newRandomToken(n)
// produces.
func validToken(token string, n int) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(n) {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(b) == n
}

// Create starts a session for the user and returns the cookie token and the
// session (its CSRFSecret is returned to the client by GET /csrf).
func (s *SessionService) Create(ctx context.Context, userID string) (string, *Session, error) {
	token, err := newRandomToken(s.rand, sessionTokenBytes)
	if err != nil {
		return "", nil, err
	}
	csrf, err := newRandomBytes(s.rand, csrfSecretBytes)
	if err != nil {
		return "", nil, err
	}
	now := s.now().UTC()
	sess := &Session{
		TokenHash:         HashSessionToken(token),
		UserID:            userID,
		CSRFSecret:        csrf,
		CreatedAt:         now,
		LastSeenAt:        now,
		ExpiresAt:         now.Add(s.ttl),
		AbsoluteExpiresAt: now.Add(s.maxAge),
	}
	if err := s.store.Create(ctx, sess); err != nil {
		return "", nil, fmt.Errorf("auth: create session: %w", err)
	}
	return token, sess, nil
}

// Authenticate resolves a cookie token to a live session and slides its idle
// expiry forward, never past the absolute lifetime. Unknown, malformed,
// expired and revoked tokens all yield ErrSessionNotFound.
func (s *SessionService) Authenticate(ctx context.Context, token string) (*Session, error) {
	if !validToken(token, sessionTokenBytes) {
		return nil, ErrSessionNotFound
	}
	hash := HashSessionToken(token)
	sess, err := s.store.Get(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("auth: get session: %w", err)
	}
	now := s.now().UTC()
	if !sess.live(now) {
		return nil, ErrSessionNotFound
	}
	if now.Sub(sess.LastSeenAt) >= s.ttl/sessionRefreshDivisor {
		expires := sess.ExpiresAt
		if next := now.Add(s.ttl); next.After(expires) {
			expires = next
		}
		if expires.After(sess.AbsoluteExpiresAt) {
			expires = sess.AbsoluteExpiresAt
		}
		if err := s.store.Extend(ctx, hash, now, expires); err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				return nil, ErrSessionNotFound
			}
			return nil, fmt.Errorf("auth: extend session: %w", err)
		}
		sess.LastSeenAt, sess.ExpiresAt = now, expires
	}
	return sess, nil
}

// Logout revokes the session of the token. Unknown tokens are ignored.
func (s *SessionService) Logout(ctx context.Context, token string) error {
	if !validToken(token, sessionTokenBytes) {
		return nil
	}
	if err := s.store.Revoke(ctx, HashSessionToken(token), s.now().UTC()); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// LogoutAll revokes every session of the user.
func (s *SessionService) LogoutAll(ctx context.Context, userID string) error {
	if err := s.store.RevokeAll(ctx, userID, s.now().UTC()); err != nil {
		return fmt.Errorf("auth: logout all: %w", err)
	}
	return nil
}

// RevokeAll implements SessionRevoker.
func (s *SessionService) RevokeAll(ctx context.Context, userID string, now time.Time) error {
	if err := s.store.RevokeAll(ctx, userID, now); err != nil {
		return fmt.Errorf("auth: revoke sessions: %w", err)
	}
	return nil
}

// Cleanup deletes sessions that are expired, past their absolute lifetime or
// revoked and returns the number of removed rows. The application calls it at
// startup and then periodically (see app/sessions_gc.go).
func (s *SessionService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.store.DeleteStale(ctx, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("auth: cleanup sessions: %w", err)
	}
	return n, nil
}
