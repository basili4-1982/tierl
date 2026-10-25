package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultResetTTL is the lifetime of a password reset link (ADR-003).
	DefaultResetTTL = time.Hour

	resetTokenBytes = 32
)

// PasswordReset is a one-time password reset token. Only the SHA-256 of the
// emailed token is stored.
type PasswordReset struct {
	TokenHash []byte
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// PasswordResets persists one-time password reset tokens.
type PasswordResets interface {
	// Create inserts a token.
	Create(ctx context.Context, t *PasswordReset) error
	// Consume atomically marks a live token used and returns its user ID.
	// Unknown, expired and already used tokens yield ErrInvalidToken.
	Consume(ctx context.Context, tokenHash []byte, now time.Time) (string, error)
	// InvalidateAll marks every live token of the user used, so that requesting
	// a new link invalidates the previous one.
	InvalidateAll(ctx context.Context, userID string, now time.Time) error
}

// Mailer delivers password reset links. TIERL-2 adds SMTP; until then
// LogMailer writes the link to the server log (development only).
type Mailer interface {
	SendPasswordReset(ctx context.Context, email, token string) error
}

// LogMailer is a development Mailer that logs the reset link instead of
// sending an email. Never use it in production.
type LogMailer struct {
	Log     *slog.Logger
	BaseURL string
}

// SendPasswordReset implements Mailer.
func (m LogMailer) SendPasswordReset(_ context.Context, email, token string) error {
	if m.Log == nil {
		return nil
	}
	link := strings.TrimRight(m.BaseURL, "/") + "/reset-password?token=" + url.QueryEscape(token)
	m.Log.Info("password reset link (development mailer)",
		slog.String("email", email), slog.String("link", link))
	return nil
}

// ResetOptions configure a PasswordResetService.
type ResetOptions struct {
	// Hash holds the argon2id parameters; zero selects DefaultHashParams.
	Hash HashParams
	// TTL is the link lifetime; default DefaultResetTTL.
	TTL time.Duration
	// Now returns the current time; default time.Now.
	Now func() time.Time
	// Rand is the entropy source; default crypto/rand.
	Rand io.Reader
	// Mailer delivers the link. Nil disables sending (tests, no-mail setups).
	Mailer Mailer
}

// PasswordResetService issues and consumes one-time password reset links.
// Resetting a password revokes every session of the user (ADR-003).
type PasswordResetService struct {
	users    Users
	resets   PasswordResets
	sessions SessionRevoker
	hasher   *Hasher
	now      func() time.Time
	rand     io.Reader
	mailer   Mailer
	ttl      time.Duration
}

// NewPasswordResetService builds a PasswordResetService.
func NewPasswordResetService(users Users, resets PasswordResets, sessions SessionRevoker, opts ResetOptions) (*PasswordResetService, error) {
	if opts.Hash == (HashParams{}) {
		opts.Hash = DefaultHashParams()
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultResetTTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	h, err := NewHasher(opts.Hash)
	if err != nil {
		return nil, err
	}
	h.rand = opts.Rand
	return &PasswordResetService{
		users: users, resets: resets, sessions: sessions, hasher: h,
		now: opts.Now, rand: opts.Rand, mailer: opts.Mailer, ttl: opts.TTL,
	}, nil
}

// MailConfigured reports whether reset links can be delivered. When false the
// HTTP layer answers 503 mail_not_configured for forgot/reset and no token is
// created or logged (production until SMTP lands in TIERL-2).
func (s *PasswordResetService) MailConfigured() bool {
	return s.mailer != nil
}

// Request issues a reset link when the email belongs to an account. It is
// deliberately silent about unknown emails: the HTTP layer always answers the
// same way, so the endpoint does not reveal whether an account exists.
func (s *PasswordResetService) Request(ctx context.Context, email string) error {
	if s.mailer == nil {
		return ErrMailNotConfigured
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		// A malformed address cannot belong to an account and the HTTP layer
		// answers the same way as for an unknown one, so there is no error.
		return nil //nolint:nilerr // uniform answer for malformed and unknown addresses
	}
	user, err := s.users.GetByEmail(ctx, normalized)
	if errors.Is(err, ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: request password reset: %w", err)
	}
	token, err := newRandomToken(s.rand, resetTokenBytes)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if err := s.resets.InvalidateAll(ctx, user.ID, now); err != nil {
		return fmt.Errorf("auth: request password reset: %w", err)
	}
	reset := &PasswordReset{
		TokenHash: hashToken(token),
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	if err := s.resets.Create(ctx, reset); err != nil {
		return fmt.Errorf("auth: request password reset: %w", err)
	}
	if s.mailer != nil {
		if err := s.mailer.SendPasswordReset(ctx, user.Email, token); err != nil {
			return fmt.Errorf("auth: send password reset: %w", err)
		}
	}
	return nil
}

// Reset sets a new password by a one-time token and revokes all sessions of
// the user. Errors: ErrInvalidToken, ErrMailNotConfigured, *ValidationError.
//
// If revoking the sessions fails after the password has been changed, the
// token is already consumed and the new password is active: the caller gets an
// error (HTTP 500) and must treat the existing sessions as not revoked.
func (s *PasswordResetService) Reset(ctx context.Context, token, newPassword string) error {
	if s.mailer == nil {
		return ErrMailNotConfigured
	}
	// Validate before consuming, so a weak password does not burn the link,
	// and before checking the token shape, so the field error wins.
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if !validToken(token, resetTokenBytes) {
		return ErrInvalidToken
	}
	now := s.now().UTC()
	userID, err := s.resets.Consume(ctx, hashToken(token), now)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			return ErrInvalidToken
		}
		return fmt.Errorf("auth: reset password: %w", err)
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	if err := s.users.UpdatePasswordHash(ctx, userID, hash, now); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrInvalidToken
		}
		return fmt.Errorf("auth: reset password: %w", err)
	}
	if s.sessions != nil {
		if err := s.sessions.RevokeAll(ctx, userID, now); err != nil {
			// The password and token are already changed/consumed here; see the
			// method comment.
			return fmt.Errorf("auth: reset password: %w", err)
		}
	}
	return nil
}

// hashToken returns the SHA-256 of a one-time token.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
