package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

func newUsers(t *testing.T) *postgres.UserStore {
	t.Helper()
	_, pool := newStore(t)
	return postgres.NewUserStore(pool)
}

func user(id, email string) *auth.User {
	return &auth.User{ID: id, Email: email, DisplayName: "Anna", PasswordHash: "$argon2id$x", CreatedAt: t0, UpdatedAt: t0}
}

func TestUsersCreateGet(t *testing.T) {
	s := newUsers(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	byEmail, err := s.GetByEmail(ctx, "anna@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if byEmail.ID != "u1" || byEmail.DisplayName != "Anna" || byEmail.EmailVerified || !byEmail.CreatedAt.Equal(t0) {
		t.Fatalf("unexpected: %+v", byEmail)
	}
	if byID, err := s.GetByID(ctx, "u1"); err != nil || byID.Email != "anna@example.com" {
		t.Fatalf("GetByID: %v %+v", err, byID)
	}
	if _, err := s.GetByID(ctx, "nope"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
	if _, err := s.GetByEmail(ctx, "nope@example.com"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestUsersDuplicateEmail(t *testing.T) {
	s := newUsers(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, user("u2", "anna@example.com")); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
	// Duplicate ID is a different constraint and must not look like a taken email.
	if err := s.Create(ctx, user("u1", "other@example.com")); err == nil || errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("want generic error, got %v", err)
	}
}

func TestUsersConstraints(t *testing.T) {
	s := newUsers(t)
	ctx := context.Background()
	upper := user("u1", "Anna@Example.com")
	if err := s.Create(ctx, upper); err == nil {
		t.Fatal("upper-case email must be rejected by the CHECK constraint")
	}
	long := user("u2", "a@example.com")
	long.DisplayName = strings.Repeat("x", 51)
	if err := s.Create(ctx, long); err == nil {
		t.Fatal("long display name must be rejected")
	}
}

func TestUsersUpdatePasswordHash(t *testing.T) {
	s := newUsers(t)
	ctx := context.Background()
	if err := s.Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	later := t0.Add(time.Hour)
	if err := s.UpdatePasswordHash(ctx, "u1", "$argon2id$new", later); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetByID(ctx, "u1")
	if got.PasswordHash != "$argon2id$new" || !got.UpdatedAt.Equal(later) {
		t.Fatalf("unexpected: %+v", got)
	}
	if err := s.UpdatePasswordHash(ctx, "missing", "x", later); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestUsersServiceEndToEnd(t *testing.T) {
	s := newUsers(t)
	ctx := context.Background()
	svc, err := auth.NewService(s, auth.Options{Hash: auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := svc.Register(ctx, auth.RegisterInput{Email: " Anna@Example.COM ", Password: "correct-horse-battery", DisplayName: "Anna"})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Email != "anna@example.com" {
		t.Fatalf("email not normalized: %q", reg.Email)
	}
	if _, err := svc.Register(ctx, auth.RegisterInput{Email: "ANNA@example.com", Password: "another-password", DisplayName: "A2"}); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
	if u, err := svc.Login(ctx, "anna@EXAMPLE.com", "correct-horse-battery"); err != nil || u.ID != reg.ID {
		t.Fatalf("login: %v %+v", err, u)
	}
	if _, err := svc.Login(ctx, "anna@example.com", "wrong-password-x"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
	if _, err := svc.Get(ctx, reg.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUsersConcurrentDuplicate(t *testing.T) {
	s := newUsers(t)
	ctx := context.Background()
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Create(ctx, user("id"+string(rune('a'+i)), "race@example.com"))
		}()
	}
	wg.Wait()
	var ok, taken int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, auth.ErrEmailTaken):
			taken++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || taken != n-1 {
		t.Fatalf("ok=%d taken=%d", ok, taken)
	}
}
