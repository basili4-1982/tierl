package auth_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/auth/authtest"
)

type resetEnv struct {
	users    *authtest.Users
	resets   *authtest.Resets
	sessions *authtest.Sessions
	mail     *authtest.Mailer
	clock    *testClock
	svc      *auth.PasswordResetService
}

func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()
	users := authtest.NewUsers()
	resets := authtest.NewResets()
	sessions := authtest.NewSessions()
	mail := &authtest.Mailer{}
	clock := newClock()
	svc, err := auth.NewPasswordResetService(users, resets, sessions, auth.ResetOptions{
		Hash:   auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
		TTL:    30 * time.Minute,
		Now:    clock.Now,
		Mailer: mail,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &resetEnv{users: users, resets: resets, sessions: sessions, mail: mail, clock: clock, svc: svc}
}

// seed registers a user with a known password and one live session.
func (e *resetEnv) seed(t *testing.T, email, password string) (*auth.User, string) {
	t.Helper()
	svc, err := auth.NewService(e.users, auth.Options{
		Hash: auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
		Now:  e.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.Register(context.Background(), auth.RegisterInput{Email: email, Password: password, DisplayName: "Anna"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessionService(e.sessions, auth.SessionOptions{TTL: time.Hour, MaxAge: 24 * time.Hour, Now: e.clock.Now})
	token, _, err := sessions.Create(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u, token
}

func TestPasswordResetFlow(t *testing.T) {
	e := newResetEnv(t)
	ctx := context.Background()
	u, session := e.seed(t, "anna@example.com", "correct-horse-battery")

	if err := e.svc.Request(ctx, "ANNA@example.com"); err != nil {
		t.Fatal(err)
	}
	mail, ok := e.mail.Last()
	if !ok || mail.To != "anna@example.com" || mail.Token == "" {
		t.Fatalf("mail = %+v, %v", mail, ok)
	}
	if e.resets.Len() != 1 {
		t.Errorf("tokens = %d", e.resets.Len())
	}

	if err := e.svc.Reset(ctx, mail.Token, "another-long-password"); err != nil {
		t.Fatal(err)
	}
	// The session is revoked.
	sessions := auth.NewSessionService(e.sessions, auth.SessionOptions{TTL: time.Hour, MaxAge: 24 * time.Hour, Now: e.clock.Now})
	if _, err := sessions.Authenticate(ctx, session); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("session survived reset: %v", err)
	}
	// The token is single-use.
	if err := e.svc.Reset(ctx, mail.Token, "third-long-password"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("reused token = %v", err)
	}
	// The new password works, the old one does not.
	users, err := auth.NewService(e.users, auth.Options{Hash: auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Login(ctx, "anna@example.com", "correct-horse-battery"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password = %v", err)
	}
	if got, err := users.Login(ctx, "anna@example.com", "another-long-password"); err != nil || got.ID != u.ID {
		t.Fatalf("new password = %v", err)
	}
}

func TestPasswordResetUnknownEmail(t *testing.T) {
	e := newResetEnv(t)
	ctx := context.Background()
	for _, email := range []string{"nobody@example.com", "not-an-email", ""} {
		if err := e.svc.Request(ctx, email); err != nil {
			t.Errorf("Request(%q) = %v", email, err)
		}
	}
	if len(e.mail.Sent) != 0 || e.resets.Len() != 0 {
		t.Errorf("sent=%d tokens=%d", len(e.mail.Sent), e.resets.Len())
	}
}

func TestPasswordResetWeakPasswordKeepsToken(t *testing.T) {
	e := newResetEnv(t)
	ctx := context.Background()
	e.seed(t, "anna@example.com", "correct-horse-battery")
	if err := e.svc.Request(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	mail, _ := e.mail.Last()
	err := e.svc.Reset(ctx, mail.Token, "short")
	var verr *auth.ValidationError
	if !errors.As(err, &verr) || verr.Field != "password" {
		t.Fatalf("weak password = %v", err)
	}
	// The link still works.
	if err := e.svc.Reset(ctx, mail.Token, "another-long-password"); err != nil {
		t.Fatalf("retry = %v", err)
	}
}

func TestPasswordResetExpiryAndInvalid(t *testing.T) {
	e := newResetEnv(t)
	ctx := context.Background()
	e.seed(t, "anna@example.com", "correct-horse-battery")
	if err := e.svc.Request(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	mail, _ := e.mail.Last()
	e.clock.Advance(31 * time.Minute)
	if err := e.svc.Reset(ctx, mail.Token, "another-long-password"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("expired token = %v", err)
	}
	for _, token := range []string{"", "abc", strings.Repeat("a", 43)} {
		if err := e.svc.Reset(ctx, token, "another-long-password"); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("token %q: %v", token, err)
		}
	}
}

func TestPasswordResetInvalidatesPreviousLink(t *testing.T) {
	e := newResetEnv(t)
	ctx := context.Background()
	e.seed(t, "anna@example.com", "correct-horse-battery")
	if err := e.svc.Request(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	first, _ := e.mail.Last()
	if err := e.svc.Request(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	second, _ := e.mail.Last()
	if first.Token == second.Token {
		t.Fatal("tokens must differ")
	}
	if err := e.svc.Reset(ctx, first.Token, "another-long-password"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("old link = %v", err)
	}
	if err := e.svc.Reset(ctx, second.Token, "another-long-password"); err != nil {
		t.Fatalf("new link = %v", err)
	}
}

func TestPasswordResetErrors(t *testing.T) {
	e := newResetEnv(t)
	ctx := context.Background()
	e.seed(t, "anna@example.com", "correct-horse-battery")

	boom := errors.New("db down")
	e.users.Errs["GetByEmail"] = boom
	if err := e.svc.Request(ctx, "anna@example.com"); !errors.Is(err, boom) {
		t.Errorf("get user = %v", err)
	}
	delete(e.users.Errs, "GetByEmail")

	e.resets.Errs["InvalidateAll"] = boom
	if err := e.svc.Request(ctx, "anna@example.com"); !errors.Is(err, boom) {
		t.Errorf("invalidate = %v", err)
	}
	delete(e.resets.Errs, "InvalidateAll")

	e.resets.Errs["Create"] = boom
	if err := e.svc.Request(ctx, "anna@example.com"); !errors.Is(err, boom) {
		t.Errorf("create = %v", err)
	}
	delete(e.resets.Errs, "Create")

	e.mail.Err = boom
	if err := e.svc.Request(ctx, "anna@example.com"); !errors.Is(err, boom) {
		t.Errorf("mailer = %v", err)
	}
	e.mail.Err = nil

	if err := e.svc.Request(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	mail, _ := e.mail.Last()
	e.resets.Errs["Consume"] = boom
	if err := e.svc.Reset(ctx, mail.Token, "another-long-password"); !errors.Is(err, boom) {
		t.Errorf("consume = %v", err)
	}
	delete(e.resets.Errs, "Consume")

	// A user deleted between request and reset invalidates the link.
	mail, _ = e.mail.Last()
	e.users.Errs["UpdatePasswordHash"] = auth.ErrUserNotFound
	if err := e.svc.Reset(ctx, mail.Token, "another-long-password"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("missing user = %v", err)
	}
	delete(e.users.Errs, "UpdatePasswordHash")
}

func TestPasswordResetNilDependencies(t *testing.T) {
	users := authtest.NewUsers()
	if _, err := auth.NewPasswordResetService(users, authtest.NewResets(), nil, auth.ResetOptions{
		Hash: auth.HashParams{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewPasswordResetService(users, authtest.NewResets(), nil, auth.ResetOptions{
		Hash: auth.HashParams{MemoryKiB: 1},
	}); err == nil {
		t.Fatal("invalid hash params must fail")
	}
}

func TestPasswordResetRandomFailure(t *testing.T) {
	users := authtest.NewUsers()
	svcUsers, err := auth.NewService(users, auth.Options{Hash: auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcUsers.Register(context.Background(), auth.RegisterInput{
		Email: "anna@example.com", Password: "correct-horse-battery", DisplayName: "Anna",
	}); err != nil {
		t.Fatal(err)
	}
	resets := authtest.NewResets()
	svc, err := auth.NewPasswordResetService(users, resets, nil, auth.ResetOptions{
		Hash: auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
		Rand: failingReader{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Request(context.Background(), "anna@example.com"); !errors.Is(err, auth.ErrRandom) {
		t.Fatalf("random = %v", err)
	}
	if resets.Len() != 0 {
		t.Error("token stored despite failure")
	}
}

func TestLogMailer(t *testing.T) {
	mailer := auth.LogMailer{BaseURL: "http://localhost:8080/"}
	if err := mailer.SendPasswordReset(context.Background(), "a@example.com", "tok"); err != nil {
		t.Fatalf("nil logger: %v", err)
	}
	mailer.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := mailer.SendPasswordReset(context.Background(), "a@example.com", "tok"); err != nil {
		t.Fatalf("send: %v", err)
	}
}
