package auth

import (
	"errors"
	"fmt"
)

// Sentinel errors. Use errors.Is to test for them.
var (
	// ErrInvalid is wrapped by every input validation error.
	ErrInvalid = errors.New("auth: invalid input")
	// ErrEmailTaken is returned when the email is already registered.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrInvalidCredentials is returned by Login for an unknown email and for a
	// wrong password alike, so callers cannot tell the two cases apart.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrUserNotFound is returned when a user does not exist.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrInvalidHash is returned when a stored hash is not a supported PHC argon2id string.
	ErrInvalidHash = errors.New("auth: invalid password hash")
	// ErrRandom is returned when the random source fails.
	ErrRandom = errors.New("auth: random source failure")
)

// ValidationError describes an invalid field. It matches ErrInvalid.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("auth: invalid %s: %s", e.Field, e.Reason)
}

// Unwrap lets errors.Is(err, ErrInvalid) succeed.
func (e *ValidationError) Unwrap() error { return ErrInvalid }
