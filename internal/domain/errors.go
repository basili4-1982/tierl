package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors. Use errors.Is to test for them.
var (
	// ErrInvalid is wrapped by every validation error.
	ErrInvalid = errors.New("domain: invalid value")
	// ErrInvalidTransition is returned for a forbidden status change.
	ErrInvalidTransition = errors.New("domain: invalid status transition")
	// ErrStructureLocked is returned when items or tiers are edited after the first answer.
	ErrStructureLocked = errors.New("domain: structure is locked after first submission")
	// ErrRandom is returned when the random source fails.
	ErrRandom = errors.New("domain: random source failure")
)

// ValidationError describes an invalid field. It matches ErrInvalid.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("domain: invalid %s: %s", e.Field, e.Reason)
}

// Unwrap lets errors.Is(err, ErrInvalid) succeed.
func (e *ValidationError) Unwrap() error { return ErrInvalid }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Reason: fmt.Sprintf(format, args...)}
}
