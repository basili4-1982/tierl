package domain

import (
	"fmt"
	"io"
	"regexp"
)

const (
	// SlugLength is the length of generated slugs (about 143 bits of entropy).
	SlugLength = 24
	// MinUnlistedSlugLength is the minimum length of an unlisted slug.
	MinUnlistedSlugLength = 12
	// MinSlugLength is the minimum length of any slug (matches api/openapi.yaml).
	MinSlugLength = 3
	// MaxSlugLength is the maximum slug length.
	MaxSlugLength = 64

	slugAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
)

var slugRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// GenerateSlug returns a URL-safe random slug of SlugLength characters read
// from r (pass crypto/rand.Reader in production).
func GenerateSlug(r io.Reader) (string, error) {
	buf := make([]byte, SlugLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRandom, err)
	}
	// The alphabet has 64 symbols, so masking the low 6 bits is unbiased.
	for i, b := range buf {
		buf[i] = slugAlphabet[b&63]
	}
	return string(buf), nil
}

// ValidateSlug checks the slug format for the given visibility.
func ValidateSlug(slug string, v Visibility) error {
	if len(slug) < MinSlugLength || len(slug) > MaxSlugLength {
		return invalid("slug", "length must be %d..%d", MinSlugLength, MaxSlugLength)
	}
	if !slugRe.MatchString(slug) {
		return invalid("slug", "only A-Z, a-z, 0-9, _ and - are allowed")
	}
	if v == VisibilityUnlisted && len(slug) < MinUnlistedSlugLength {
		return invalid("slug", "unlisted slug must be at least %d characters", MinUnlistedSlugLength)
	}
	return nil
}
