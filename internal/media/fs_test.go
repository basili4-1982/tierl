package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func newTestFS(t *testing.T) (*FS, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "media")
	s, err := NewFS(dir, "/media/")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func TestFS_Contract(t *testing.T) {
	s, _ := newTestFS(t)
	storageContract(t, s, "/media/")
}

// storageContract is shared by all Storage implementations.
func storageContract(t *testing.T, s Storage, urlPrefix string) {
	t.Helper()
	ctx := context.Background()
	data := []byte("payload-bytes")

	if _, err := s.Get(ctx, "missing.png"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
	if err := s.Put(ctx, "a.png", data, TypePNG); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(ctx, "a.png", append(data, '!'), TypePNG); err != nil {
		t.Fatalf("Put overwrite: %v", err)
	}
	if err := s.Put(ctx, "sub/dir/b.png", data, TypePNG); err != nil {
		t.Fatalf("Put nested: %v", err)
	}
	for key, want := range map[string]string{"a.png": "payload-bytes!", "sub/dir/b.png": "payload-bytes"} {
		rc, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(got) != want {
			t.Errorf("Get %s = %q, want %q", key, got, want)
		}
	}
	if got := s.URL("a.png"); got != urlPrefix+"a.png" {
		t.Errorf("URL = %q", got)
	}
	if err := s.Delete(ctx, "a.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, "a.png"); err != nil {
		t.Fatalf("Delete twice: %v", err)
	}
	if _, err := s.Get(ctx, "a.png"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
	for _, bad := range badKeys {
		if err := s.Put(ctx, bad, data, TypePNG); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", bad, err)
		}
		if _, err := s.Get(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Get(%q) = %v, want ErrInvalidKey", bad, err)
		}
		if err := s.Delete(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q) = %v, want ErrInvalidKey", bad, err)
		}
	}
}

var badKeys = []string{
	"", "../evil.png", "a/../../evil.png", "/etc/passwd", "..", "a/..", "a//b.png",
	"a/b/", ".hidden", "a\\b.png", "a\x00b.png", "sp ace.png", "ключ.png", string(bytes.Repeat([]byte("a"), 300)),
}

func TestFS_PathTraversalNeverTouchesOutside(t *testing.T) {
	s, dir := newTestFS(t)
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(outside, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Get(ctx, "../secret.txt"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Get traversal: %v", err)
	}
	if err := s.Delete(ctx, "../secret.txt"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete traversal: %v", err)
	}
	if err := s.Put(ctx, "../pwned.png", []byte("x"), TypePNG); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Put traversal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "pwned.png")); err == nil {
		t.Error("file written outside the root")
	}
	if b, _ := os.ReadFile(outside); string(b) != "top secret" {
		t.Error("outside file modified")
	}
}

// A symlink inside the root pointing outside must not be followed (os.Root).
func TestFS_SymlinkEscape(t *testing.T) {
	s, dir := newTestFS(t)
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ctx := context.Background()
	if rc, err := s.Get(ctx, "link/secret.txt"); err == nil {
		_ = rc.Close()
		t.Error("Get followed a symlink out of the root")
	}
	if err := s.Put(ctx, "link/new.png", []byte("x"), TypePNG); err == nil {
		t.Error("Put followed a symlink out of the root")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new.png")); err == nil {
		t.Error("file written outside the root")
	}
}

func TestFS_CancelledContext(t *testing.T) {
	s, _ := newTestFS(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Put(ctx, "a.png", nil, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("Put: %v", err)
	}
	if _, err := s.Get(ctx, "a.png"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get: %v", err)
	}
	if err := s.Delete(ctx, "a.png"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: %v", err)
	}
}

func TestFS_NoTempFilesLeft(t *testing.T) {
	s, dir := newTestFS(t)
	if err := s.Put(context.Background(), "a.png", []byte("x"), TypePNG); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "a.png" {
		t.Errorf("unexpected dir content: %v", entries)
	}
}

func TestFS_Errors(t *testing.T) {
	// Storage dir cannot be created under a regular file.
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFS(filepath.Join(f, "sub"), "/m"); err == nil {
		t.Error("NewFS under a file must fail")
	}

	s, _ := newTestFS(t)
	ctx := context.Background()
	// "a" is a file; "a/b.png" needs "a" as a directory.
	if err := s.Put(ctx, "a", []byte("x"), ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "a/b.png", []byte("x"), ""); err == nil {
		t.Error("Put with a file as parent dir must fail")
	}
	if _, err := s.Get(ctx, "a/b.png"); err == nil {
		t.Error("Get below a file must fail")
	}
	// Directory as object: rename onto a non-empty dir fails, Get returns the dir handle.
	if err := s.Put(ctx, "d/x.png", []byte("x"), ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "d", []byte("x"), ""); err == nil {
		t.Error("Put over a directory must fail")
	}
	if err := s.Delete(ctx, "d"); err == nil {
		t.Error("Delete of a non-empty directory must fail")
	}
}
