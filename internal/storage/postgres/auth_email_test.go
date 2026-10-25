package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/mailer"
	"github.com/basili4-1982/tierl/internal/platform/mailer/mailertest"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

type recordingRevoker struct {
	mu    sync.Mutex
	calls int
	user  string
}

func (r *recordingRevoker) LogoutAll(_ context.Context, userID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.user = userID
	return 1, nil
}

func (r *recordingRevoker) snapshot() (int, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.user
}

var linkTokenRE = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func linkToken(t *testing.T, m mailer.Message) string {
	t.Helper()
	if sub := linkTokenRE.FindStringSubmatch(m.Text); sub != nil {
		return sub[1]
	}
	t.Fatalf("no token in %q", m.Text)
	return ""
}

// TestEmailConfirmationAndResetOnRealDatabase runs both flows against the
// migrations and the PostgreSQL repositories.
func TestEmailConfirmationAndResetOnRealDatabase(t *testing.T) {
	_, pool := newStore(t)
	ctx := context.Background()
	users := postgres.NewUserStore(pool)
	tokens := postgres.NewTokenStore(pool)
	capture := &mailertest.Capture{}
	revoker := &recordingRevoker{}
	now := t0

	svc, err := auth.NewService(users, auth.Options{
		Hash:     auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
		Now:      func() time.Time { return now },
		Tokens:   tokens,
		Mail:     capture,
		Sessions: revoker,
		BaseURL:  "https://tierl.example",
		Lang:     mailer.LangRU,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	anna, err := svc.Register(ctx, auth.RegisterInput{Email: "Anna@Example.com", Password: "correct-horse-battery", DisplayName: "Анна"})
	if err != nil {
		t.Fatal(err)
	}
	if capture.Len() != 1 {
		t.Fatalf("verification letters = %d", capture.Len())
	}
	verifyMsg, _ := capture.Last()
	if verifyMsg.To != "anna@example.com" {
		t.Fatalf("letter recipient = %q", verifyMsg.To)
	}
	verifyToken := linkToken(t, verifyMsg)

	// A forged token changes nothing.
	if err := svc.VerifyEmail(ctx, "forged"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("forged token: %v", err)
	}
	if got, _ := users.GetByID(ctx, anna.ID); got.EmailVerified {
		t.Fatal("forged token must not verify")
	}

	if err := svc.VerifyEmail(ctx, verifyToken); err != nil {
		t.Fatal(err)
	}
	got, err := users.GetByID(ctx, anna.ID)
	if err != nil || !got.EmailVerified {
		t.Fatalf("after verify: %+v %v", got, err)
	}
	if err := svc.VerifyEmail(ctx, verifyToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("reused token: %v", err)
	}

	// An expired confirmation link.
	bob, err := svc.Register(ctx, auth.RegisterInput{Email: "bob@example.com", Password: "correct-horse-battery", DisplayName: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	bobMsg, _ := capture.Last()
	bobToken := linkToken(t, bobMsg)
	if _, err := pool.Exec(ctx, `
		UPDATE one_time_tokens SET created_at = $2, expires_at = $3
		WHERE user_id = $1 AND purpose = 'verify_email' AND used_at IS NULL`,
		bob.ID, t0.Add(-2*time.Hour), t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(ctx, bobToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("expired token: %v", err)
	}

	// Password reset.
	before := capture.Len()
	if err := svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	if capture.Len() != before+1 {
		t.Fatalf("reset letters = %d", capture.Len()-before)
	}
	resetMsg, _ := capture.Last()
	resetToken := linkToken(t, resetMsg)

	if err := svc.ResetPassword(ctx, resetToken, "another-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "anna@example.com", "another-long-password"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if _, err := svc.Login(ctx, "anna@example.com", "correct-horse-battery"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if calls, user := revoker.snapshot(); calls != 1 || user != anna.ID {
		t.Fatalf("sessions revoked %d times for %q", calls, user)
	}
	var active int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM one_time_tokens
		WHERE user_id = $1 AND purpose = 'reset_password' AND used_at IS NULL`, anna.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("%d reset tokens left active", active)
	}
	if err := svc.ResetPassword(ctx, resetToken, "yet-another-password"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("reused reset token: %v", err)
	}

	// A second request invalidates the first link.
	if err := svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	firstMsg, _ := capture.Last()
	if err := svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	secondMsg, _ := capture.Last()
	if err := svc.ResetPassword(ctx, linkToken(t, firstMsg), "third-long-password"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("superseded token: %v", err)
	}
	if err := svc.ResetPassword(ctx, linkToken(t, secondMsg), "third-long-password"); err != nil {
		t.Fatalf("latest token: %v", err)
	}

	// Unknown addresses are indistinguishable in the response and send nothing.
	before = capture.Len()
	if err := svc.ForgotPassword(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("unknown email: %v", err)
	}
	if capture.Len() != before {
		t.Fatal("unknown email must not receive a letter")
	}
}
