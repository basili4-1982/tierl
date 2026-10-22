package auth

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Input limits (SPEC section 6, openapi RegisterRequest).
const (
	MinPasswordLen    = 10
	MaxPasswordLen    = 128
	MaxEmailLen       = 254
	MinDisplayNameLen = 1
	MaxDisplayNameLen = 50
)

// NormalizeEmail trims and lower-cases an email and checks its shape.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", &ValidationError{Field: "email", Reason: "must not be empty"}
	}
	if len(email) > MaxEmailLen {
		return "", &ValidationError{Field: "email", Reason: "too long"}
	}
	addr, err := mail.ParseAddress(email)
	// Reject display names ("Bob <b@x.y>") and anything but a bare address.
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", &ValidationError{Field: "email", Reason: "not a valid address"}
	}
	return email, nil
}

// ValidatePassword checks the length in characters (not bytes).
func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLen || n > MaxPasswordLen {
		return &ValidationError{Field: "password", Reason: "length must be 10 to 128 characters"}
	}
	return nil
}

// NormalizeDisplayName trims a display name and checks its length.
func NormalizeDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	n := utf8.RuneCountInString(name)
	if n < MinDisplayNameLen || n > MaxDisplayNameLen {
		return "", &ValidationError{Field: "display_name", Reason: "length must be 1 to 50 characters"}
	}
	return name, nil
}
