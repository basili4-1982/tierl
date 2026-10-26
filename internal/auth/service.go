// Package auth implements users, registration and login with argon2id
// password hashes (ADR-003). Sessions, email confirmation and password reset
// live elsewhere. The storage interface is declared here, on the consumer
// side; internal/storage/postgres implements it.
package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"
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

// Options configure a Service. Zero values select safe defaults.
type Options struct {
	// Hash holds the argon2id parameters; the zero value means DefaultHashParams.
	Hash HashParams
	// Now returns the current time; default time.Now.
	Now func() time.Time
	// Rand is the entropy source for IDs and salts; default crypto/rand.
	Rand io.Reader
	// Sessions, when set, lets ChangePassword revoke every session of a user.
	Sessions SessionRevoker
}

// Service registers and authenticates users.
type Service struct {
	users    Users
	sessions SessionRevoker
	hasher   *Hasher
	now      func() time.Time
	rand     io.Reader
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
	h, err := NewHasher(opts.Hash)
	if err != nil {
		return nil, err
	}
	h.rand = opts.Rand
	dummy, err := h.Hash("tierl-dummy-password-for-timing")
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	return &Service{users: users, sessions: opts.Sessions, hasher: h, now: opts.Now, rand: opts.Rand, dummyHash: dummy}, nil
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

// ResetPassword sets a new password without checking the old one (for the
// email reset flow) and revokes every session of the user.
func (s *Service) ResetPassword(ctx context.Context, userID, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	if err := s.storePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	return nil
}

// SetPassword is an alias for ResetPassword.
func (s *Service) SetPassword(ctx context.Context, userID, newPassword string) error {
	return s.ResetPassword(ctx, userID, newPassword)
}

func (s *Service) storePassword(ctx context.Context, userID, hash string) error {
	now := s.now().UTC()
	if err := s.users.UpdatePasswordHash(ctx, userID, hash, now); err != nil {
		return err
	}
	if s.sessions != nil {
		if err := s.sessions.DeleteByUserID(ctx, userID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
	}
	return nil
}
