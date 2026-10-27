// Package auth implements users, registration and login with argon2id
// password hashes (ADR-003). Sessions, email confirmation and password reset
// live here too. The storage interface is declared here, on the consumer
// side; internal/storage/postgres implements it.
package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"code.sarnas.ru/tierl/internal/platform/mailer"
)

// User is an account.
type User struct {
	ID            string
	Email         string // lower-case
	DisplayName   string
	PasswordHash  string // PHC string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Users persists accounts.
type Users interface {
	// Create inserts a user. It returns ErrEmailTaken when the email exists.
	Create(ctx context.Context, u *User) error
	// GetByEmail returns the user with the given (already lower-cased) email or ErrUserNotFound.
	GetByEmail(ctx context.Context, email string) (*User, error)
	// GetByID returns the user with the given ID or ErrUserNotFound.
	GetByID(ctx context.Context, id string) (*User, error)
	// UpdatePasswordHash replaces the stored hash or returns ErrUserNotFound.
	UpdatePasswordHash(ctx context.Context, id, hash string, now time.Time) error
}

// EmailMarker is implemented by user stores that can confirm an address.
// It is optional so existing Users implementations keep working.
type EmailMarker interface {
	// MarkEmailVerified sets email_verified and updated_at or returns ErrUserNotFound.
	MarkEmailVerified(ctx context.Context, id string, now time.Time) error
}

// PasswordSessions is implemented by user stores that can change the password
// hash and delete all other sessions of the account in one transaction.
type PasswordSessions interface {
	UpdatePasswordAndDeleteSessions(ctx context.Context, userID, hash, keepTokenHash string, now time.Time) (int64, error)
}

// OtherSessionDeleter is implemented by session stores that can remove every
// session of a user except the one identified by keepTokenHash.
type OtherSessionDeleter interface {
	DeleteOthersByUserID(ctx context.Context, userID, keepTokenHash string) (int64, error)
}

// Options configure a Service. Zero values select safe defaults.
type Options struct {
	// Hash holds the argon2id parameters; the zero value means DefaultHashParams.
	Hash HashParams
	// Now returns the current time; default time.Now.
	Now func() time.Time
	// Rand is the entropy source for IDs and salts; default crypto/rand.
	Rand io.Reader
	// Tokens stores one-time email tokens. Without it the email confirmation
	// and password reset methods return ErrNotConfigured.
	Tokens TokenStore
	// Mail delivers letter templates. Nil disables sending; Register still
	// succeeds, but nothing is delivered.
	Mail Mailer
	// Sessions revokes every session of a user after a password reset. It
	// accepts either a SessionManager (DeleteByUserID) or a LogoutAll revoker.
	Sessions any
	// BaseURL is the public origin used in email links; default
	// http://localhost:8080.
	BaseURL string
	// Lang is the letter language; default Russian.
	Lang mailer.Lang
	// Log receives delivery failures. Tokens and links are never logged.
	Log *slog.Logger
}

// Service registers and authenticates users.
type Service struct {
	users    Users
	hasher   *Hasher
	now      func() time.Time
	rand     io.Reader
	tokens   TokenStore
	mail     Mailer
	sessions any
	baseURL  string
	lang     mailer.Lang
	log      *slog.Logger
	// dummyHash is verified when the email is unknown, so that response time
	// does not reveal whether the account exists.
	dummyHash string
}

// NewService builds a Service.
func NewService(users Users, opts Options) (*Service, error) {
	if opts.Hash == (HashParams{}) {
		opts.Hash = DefaultHashParams()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "http://localhost:8080"
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.Lang == "" {
		opts.Lang = mailer.LangRU
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	h, err := NewHasher(opts.Hash)
	if err != nil {
		return nil, err
	}
	h.rand = opts.Rand
	dummy, err := h.Hash("tierl-dummy-password-for-timing")
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	return &Service{
		users: users, hasher: h, now: opts.Now, rand: opts.Rand,
		tokens: opts.Tokens, mail: opts.Mail, sessions: opts.Sessions,
		baseURL: opts.BaseURL, lang: opts.Lang, log: opts.Log,
		dummyHash: dummy,
	}, nil
}

// RegisterInput is the data of a new account.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

// Register validates the input, hashes the password and creates the user.
// Errors: *ValidationError (ErrInvalid), ErrEmailTaken.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*User, error) {
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	name, err := NormalizeDisplayName(in.DisplayName)
	if err != nil {
		return nil, err
	}
	id, err := newID(s.rand)
	if err != nil {
		return nil, err
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, fmt.Errorf("auth: register: %w", err)
	}
	now := s.now().UTC()
	u := &User{ID: id, Email: email, DisplayName: name, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("auth: register: %w", err)
	}
	s.notifyVerification(ctx, u)
	return u, nil
}

// Login checks the credentials. An unknown email and a wrong password both
// return ErrInvalidCredentials, and an unknown email still pays for one hash
// verification. When the stored hash uses outdated parameters it is upgraded
// after a successful check; a failed upgrade does not fail the login.
func (s *Service) Login(ctx context.Context, email, password string) (*User, error) {
	norm, nerr := NormalizeEmail(email)
	if nerr != nil || len(password) > 4*MaxPasswordLen {
		// Malformed input cannot belong to an account; keep the cost uniform.
		_, _ = s.hasher.Verify(truncate(password), s.dummyHash)
		return nil, ErrInvalidCredentials
	}
	u, err := s.users.GetByEmail(ctx, norm)
	if errors.Is(err, ErrUserNotFound) {
		_, _ = s.hasher.Verify(password, s.dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("auth: login: %w", err)
	}
	ok, err := s.hasher.Verify(password, u.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("auth: login: %w", err)
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	if stale, err := s.hasher.NeedsRehash(u.PasswordHash); err == nil && stale {
		if hash, err := s.hasher.Hash(password); err == nil {
			now := s.now().UTC()
			if err := s.users.UpdatePasswordHash(ctx, u.ID, hash, now); err == nil {
				u.PasswordHash, u.UpdatedAt = hash, now
			}
		}
	}
	return u, nil
}

// Get returns a user by ID or ErrUserNotFound.
func (s *Service) Get(ctx context.Context, id string) (*User, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	return u, nil
}

func truncate(p string) string {
	if len(p) > 4*MaxPasswordLen {
		return p[:4*MaxPasswordLen]
	}
	return p
}

// newID returns a random UUID v4.
func newID(r io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRandom, err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// ChangePassword verifies the current password, stores a hash of the new one
// and revokes every session of the user, including the current one.
// Errors: ErrInvalidCredentials when the old password is wrong,
// *ValidationError (ErrInvalid) when the new password is invalid.
func (s *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := s.hasher.Verify(oldPassword, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("auth: change password: %w", err)
	}
	if !ok {
		return ErrInvalidCredentials
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: change password: %w", err)
	}
	if err := s.storePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("auth: change password: %w", err)
	}
	return nil
}

// ChangePasswordKeepingCurrent changes the password and revokes every session
// of the user except the current one (identified by its raw cookie token).
// It returns the number of revoked sessions when the store can report it.
// Errors are the same as ChangePassword.
func (s *Service) ChangePasswordKeepingCurrent(ctx context.Context, userID, currentToken, oldPassword, newPassword string) (int64, error) {
	if err := ValidatePassword(newPassword); err != nil {
		return 0, err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	ok, err := s.hasher.Verify(oldPassword, u.PasswordHash)
	if err != nil {
		return 0, fmt.Errorf("auth: change password: %w", err)
	}
	if !ok {
		return 0, ErrInvalidCredentials
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return 0, fmt.Errorf("auth: change password: %w", err)
	}
	now := s.now().UTC()
	keep := HashToken(currentToken)
	if store, ok := s.users.(PasswordSessions); ok {
		n, err := store.UpdatePasswordAndDeleteSessions(ctx, userID, hash, keep, now)
		if err != nil {
			return 0, fmt.Errorf("auth: change password: %w", err)
		}
		return n, nil
	}
	if err := s.users.UpdatePasswordHash(ctx, userID, hash, now); err != nil {
		return 0, fmt.Errorf("auth: change password: %w", err)
	}
	return s.deleteOtherSessions(ctx, userID, keep)
}

// deleteOtherSessions is the non-transactional fallback for stores that do not
// implement PasswordSessions. The current session is kept when the session
// store supports it.
func (s *Service) deleteOtherSessions(ctx context.Context, userID, keepTokenHash string) (int64, error) {
	switch r := s.sessions.(type) {
	case nil:
		return 0, nil
	case OtherSessionDeleter:
		n, err := r.DeleteOthersByUserID(ctx, userID, keepTokenHash)
		if err != nil {
			return 0, fmt.Errorf("revoke sessions: %w", err)
		}
		return n, nil
	case interface {
		DeleteByUserID(ctx context.Context, userID string) error
	}:
		if err := r.DeleteByUserID(ctx, userID); err != nil {
			return 0, fmt.Errorf("revoke sessions: %w", err)
		}
		return 0, nil
	default:
		return 0, errors.New("auth: unsupported session revoker")
	}
}

// SetPassword sets a new password without checking the old one and revokes
// every session of the user. It is kept for administrative callers; the email
// reset flow is ResetPassword(ctx, token, newPassword).
func (s *Service) SetPassword(ctx context.Context, userID, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: set password: %w", err)
	}
	if err := s.storePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("auth: set password: %w", err)
	}
	return nil
}

// ResetPasswordForUser is an alias for SetPassword.
func (s *Service) ResetPasswordForUser(ctx context.Context, userID, newPassword string) error {
	return s.SetPassword(ctx, userID, newPassword)
}

func (s *Service) storePassword(ctx context.Context, userID, hash string) error {
	now := s.now().UTC()
	if err := s.users.UpdatePasswordHash(ctx, userID, hash, now); err != nil {
		return err
	}
	if err := s.revokeSessions(ctx, userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

// revokeSessions supports both the session manager's DeleteByUserID and the
// LogoutAll signature used by the email reset flow.
func (s *Service) revokeSessions(ctx context.Context, userID string) error {
	switch r := s.sessions.(type) {
	case nil:
		return nil
	case interface {
		LogoutAll(ctx context.Context, userID string) (int64, error)
	}:
		_, err := r.LogoutAll(ctx, userID)
		return err
	case interface {
		DeleteByUserID(ctx context.Context, userID string) error
	}:
		return r.DeleteByUserID(ctx, userID)
	default:
		return errors.New("auth: unsupported session revoker")
	}
}
