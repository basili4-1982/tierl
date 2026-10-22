package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
)

// FS is a Storage backed by a local directory. All file access goes through
// os.Root, so a key can never reach outside the directory (also via symlinks).
type FS struct {
	root    *os.Root
	baseURL string
}

// NewFS opens (creating if needed) dir as the storage root. baseURL is the
// public prefix returned by URL, e.g. "/media".
func NewFS(dir, baseURL string) (*FS, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("media: create storage dir: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("media: open storage dir: %w", err)
	}
	return &FS{root: root, baseURL: baseURL}, nil
}

// Close releases the root directory handle.
func (s *FS) Close() error { return s.root.Close() }

// Put writes the object atomically (temp file + rename). contentType is
// ignored: the type is derived from the extension on serving.
func (s *FS) Put(ctx context.Context, key string, data []byte, _ string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if dir := path.Dir(key); dir != "." {
		if err := s.root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("media: mkdir: %w", err)
		}
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return fmt.Errorf("media: temp name: %w", err)
	}
	tmp := key + ".tmp-" + hex.EncodeToString(rnd[:])
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("media: create temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = s.root.Remove(tmp)
		return fmt.Errorf("media: write: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("media: close: %w", err)
	}
	if err := s.root.Rename(tmp, key); err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("media: rename: %w", err)
	}
	return nil
}

// Get opens the object.
func (s *FS) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := s.root.Open(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("media: open: %w", err)
	}
	return f, nil
}

// Delete removes the object; a missing object is not an error.
func (s *FS) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("media: remove: %w", err)
	}
	return nil
}

// URL returns baseURL/key.
func (s *FS) URL(key string) string { return joinURL(s.baseURL, key) }
