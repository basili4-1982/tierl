package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/basili4-1982/tierl/internal/platform/mailer"
)

// One-time token lifetimes. Email confirmation is valid for a day; a password
// reset link is short-lived.
const (
	VerifyEmailTTL   = 24 * time.Hour
	ResetPasswordTTL = time.Hour

	oneTimeTokenBytes = 32
)

// Purpose tells the two one-time token flows apart. The value is stored in the
// database, so it is stable.
type Purpose string

// Token purposes.
const (
	PurposeVerifyEmail   Purpose = "verify_email"
	PurposeResetPassword Purpose = "reset_password"
)

// OneTimeToken is a stored token. TokenHash is the SHA-256 of the raw token;
// the raw value travels only in the email.
type OneTimeToken struct {
	ID        string
	UserID    string
	Purpose   Purpose
	TokenHash []byte
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// TokenStore persists one-time tokens.
type TokenStore interface {
	// Issue stores t and marks every still-active token of the same user and
	// purpose used, so only the newest token works.
	Issue(ctx context.Context, t *OneTimeToken) error
	// Consume atomically marks a valid token used and returns it. It returns
	// ErrInvalidToken for an unknown, expired or already used token, and for a
	// token of another purpose.
	Consume(ctx context.Context, tokenHash []byte, purpose Purpose, now time.Time) (*OneTimeToken, error)
	// Invalidate marks all unused tokens of the user and purpose used.
	Invalidate(ctx context.Context, userID string, purpose Purpose, now time.Time) error
}

// Mailer delivers a rendered letter. *mailer.SMTPSender and mailer.LogSender
// implement the underlying transport; tests use mailertest.Capture.
type Mailer interface {
	Send(ctx context.Context, m mailer.Message) error
}

// SessionRevoker ends every server-side session of a user. The sessions
// module's service implements LogoutAll with this exact signature.
type SessionRevoker interface {
	LogoutAll(ctx context.Context, userID string) (int64, error)
}

// VerifyEmail confirms the email of the token owner. A token is single-use and
// expires after VerifyEmailTTL. Errors: ErrInvalidToken, ErrNotConfigured.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	if s.tokens == nil {
		return ErrNotConfigured
	}
	hash, ok := hashOneTimeToken(token)
	if !ok {
		return ErrInvalidToken
	}
	now := s.now().UTC()
	stored, err := s.tokens.Consume(ctx, hash, PurposeVerifyEmail, now)
	if errors.Is(err, ErrInvalidToken) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}
	// A verified address makes any other confirmation link pointless.
	if err := s.tokens.Invalidate(ctx, stored.UserID, PurposeVerifyEmail, now); err != nil {
		return fmt.Errorf("auth: verify email: invalidate other tokens: %w", err)
	}
	if err := s.users.MarkEmailVerified(ctx, stored.UserID, now); err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}
	return nil
}

// ResendVerification sends a fresh confirmation link. It is deliberately
// silent for an unknown email and for an already verified one, so the endpoint
// does not reveal which addresses exist. Errors: *ValidationError,
// ErrNotConfigured (only for a malformed request the caller wants to report).
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	if s.tokens == nil || s.mail == nil {
		return ErrNotConfigured
	}
	norm, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	u, err := s.users.GetByEmail(ctx, norm)
	if errors.Is(err, ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: resend verification: %w", err)
	}
	if u.EmailVerified {
		return nil
	}
	if err := s.issueAndSend(ctx, u, PurposeVerifyEmail, VerifyEmailTTL); err != nil {
		// Delivery failures must not turn into an existence oracle.
		s.log.ErrorContext(ctx, "auth: resend verification failed",
			slog.String("user_id", u.ID), slog.Any("error", err))
	}
	return nil
}

// ForgotPassword sends a password reset link. The response is identical for a
// known and an unknown email: an unknown address and a delivery failure are
// swallowed after logging (without the token). Errors: *ValidationError.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	if s.tokens == nil || s.mail == nil {
		return ErrNotConfigured
	}
	norm, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	u, err := s.users.GetByEmail(ctx, norm)
	if errors.Is(err, ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: forgot password: %w", err)
	}
	if err := s.issueAndSend(ctx, u, PurposeResetPassword, ResetPasswordTTL); err != nil {
		s.log.ErrorContext(ctx, "auth: forgot password failed",
			slog.String("user_id", u.ID), slog.Any("error", err))
	}
	return nil
}

// ResetPassword sets a new password using a reset token. The token is consumed
// atomically, every other reset token of the user is invalidated and all
// sessions are revoked. Errors: ErrInvalidToken, *ValidationError,
// ErrNotConfigured.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if s.tokens == nil {
		return ErrNotConfigured
	}
	// Validate before consuming, so a short password does not burn the link.
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, ok := hashOneTimeToken(token)
	if !ok {
		return ErrInvalidToken
	}
	now := s.now().UTC()
	stored, err := s.tokens.Consume(ctx, hash, PurposeResetPassword, now)
	if errors.Is(err, ErrInvalidToken) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	passwordHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	// Revoke sessions before changing the hash: if revocation fails, the
	// password is untouched and the user can request another link.
	if s.sessions != nil {
		if _, err := s.sessions.LogoutAll(ctx, stored.UserID); err != nil {
			return fmt.Errorf("auth: reset password: revoke sessions: %w", err)
		}
	}
	if err := s.tokens.Invalidate(ctx, stored.UserID, PurposeResetPassword, now); err != nil {
		return fmt.Errorf("auth: reset password: invalidate other tokens: %w", err)
	}
	if err := s.users.UpdatePasswordHash(ctx, stored.UserID, passwordHash, now); err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	return nil
}

// notifyVerification sends a confirmation letter after registration. The
// account is created even when delivery fails; the user can resend later.
func (s *Service) notifyVerification(ctx context.Context, u *User) {
	if s.tokens == nil || s.mail == nil {
		return
	}
	if err := s.issueAndSend(ctx, u, PurposeVerifyEmail, VerifyEmailTTL); err != nil {
		s.log.ErrorContext(ctx, "auth: send verification email failed",
			slog.String("user_id", u.ID), slog.Any("error", err))
	}
}

// issueAndSend creates a token (invalidating the previous one) and delivers
// the letter. The error never contains the token or the link.
func (s *Service) issueAndSend(ctx context.Context, u *User, purpose Purpose, ttl time.Duration) error {
	raw, hash, err := newOneTimeToken(s.rand)
	if err != nil {
		return err
	}
	id, err := newID(s.rand)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	t := &OneTimeToken{
		ID: id, UserID: u.ID, Purpose: purpose, TokenHash: hash,
		ExpiresAt: now.Add(ttl), CreatedAt: now,
	}
	if err := s.tokens.Issue(ctx, t); err != nil {
		return fmt.Errorf("auth: issue %s token: %w", purpose, err)
	}
	link := s.link(purpose, raw)
	msg, err := mailer.Render(s.lang, kindFor(purpose), mailer.Data{
		Name: u.DisplayName,
		URL:  link,
		TTL:  mailer.FormatTTL(s.lang, ttl),
	})
	if err != nil {
		return err
	}
	msg.To = u.Email
	if err := s.mail.Send(ctx, msg); err != nil {
		return fmt.Errorf("auth: send %s email: %w", purpose, err)
	}
	return nil
}

func (s *Service) link(purpose Purpose, token string) string {
	path := "/verify-email"
	if purpose == PurposeResetPassword {
		path = "/reset-password"
	}
	base, err := url.Parse(s.baseURL)
	if err != nil {
		base = &url.URL{}
	}
	u := *base
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func kindFor(purpose Purpose) mailer.Kind {
	if purpose == PurposeResetPassword {
		return mailer.ResetPassword
	}
	return mailer.VerifyEmail
}

// newOneTimeToken returns a raw 32-byte token and its SHA-256.
func newOneTimeToken(r io.Reader) (raw string, hash []byte, err error) {
	var b [oneTimeTokenBytes]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrRandom, err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b[:])
	h := sha256.Sum256([]byte(raw))
	return raw, h[:], nil
}

// hashOneTimeToken validates the shape and returns the stored hash.
func hashOneTimeToken(token string) ([]byte, bool) {
	if base64.RawURLEncoding.EncodedLen(oneTimeTokenBytes) != len(token) {
		return nil, false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(b) != oneTimeTokenBytes {
		return nil, false
	}
	h := sha256.Sum256([]byte(token))
	return h[:], true
}
