// Package media stores and sanitises user-uploaded images.
//
// Process validates and re-encodes an upload; Storage persists the result
// either on the local filesystem or in an S3-compatible bucket.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Storage errors.
var (
	// ErrNotFound is returned by Get when the object does not exist.
	ErrNotFound = errors.New("media: object not found")
	// ErrInvalidKey is returned for keys that are empty, absolute, contain
	// ".." or characters outside [A-Za-z0-9._-/].
	ErrInvalidKey = errors.New("media: invalid object key")
)

// Storage is a blob store for processed images.
type Storage interface {
	// Put stores data under key, replacing any existing object.
	Put(ctx context.Context, key string, data []byte, contentType string) error
	// Get opens the object; the caller must close the reader. Missing
	// objects yield ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// URL returns the public URL the object is served from.
	URL(key string) string
}

var keyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)

// ValidateKey rejects keys that could escape the storage root or are
// otherwise unsafe. Every Storage implementation calls it first.
func ValidateKey(key string) error {
	if len(key) > 255 || !keyRe.MatchString(key) || strings.Contains(key, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return nil
}

func joinURL(base, key string) string {
	return strings.TrimRight(base, "/") + "/" + key
}
