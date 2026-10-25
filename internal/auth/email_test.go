package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/platform/mailer"
	"github.com/basili4-1982/tierl/internal/platform/mailer/mailertest"
)

// memTokens is an in-memory TokenStore with the same semantics as the
// PostgreSQL one.
type memTokens struct {
	mu      sync.Mutex
	byHash  map[string]*OneTimeToken
	failOn  string
	failErr error
}

func newMemTokens() *memTokens { return &memTokens{byHash: map[string]*OneTimeToken{}} }

func (m *memTokens) Issue(_ context.Context, t *OneTimeToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn == "issue" {
		return m.failErr
	}
	now := t.CreatedAt
	for _, old := range m.byHash {
		if old.UserID == t.UserID && old.Purpose == t.Purpose && old.UsedAt == nil {
			used := now
			old.UsedAt = &used
		}
	}
	c := *t
	m.byHash[hex.EncodeToString(t.TokenHash)] = &c
	return nil
}

func (m *memTokens) Consume(_ context.Context, hash []byte, purpose Purpose, now time.Time) (*OneTimeToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn == "consume" {
		return nil, m.failErr
	}
	t, ok := m.byHash[hex.EncodeToString(hash)]
	if !ok || t.Purpose != purpose || t.UsedAt != nil || !t.ExpiresAt.After(now) {
		return nil, ErrInvalidToken
	}
	used := now
	t.UsedAt = &used
	c := *t
	return &c, nil
}

func (m *memTokens) Invalidate(_ context.Context, userID string, purpose Purpose, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn == "invalidate" {
		return m.failErr
	}
	for _, t := range m.byHash {
		if t.UserID == userID && t.Purpose == purpose && t.UsedAt == nil {
			used := now
			t.UsedAt = &used
		}
	}
	return nil
}

func (m *memTokens) active(userID string, purpose Purpose) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.byHash {
		if t.UserID == userID && t.Purpose == purpose && t.UsedAt == nil {
			n++
		}
	}
	return n
}

type fakeRevoker struct {
	calls int
	user  string
	err   error
}

func (f *fakeRevoker) LogoutAll(_ context.Context, userID string) (int64, error) {
	f.calls++
	f.user = userID
	return 3, f.err
}

type emailEnv struct {
	svc      *Service
	users    *memUsers
	tokens   *memTokens
	mail     *mailertest.Capture
	sessions *fakeRevoker
	now      time.Time
	logs     *bytes.Buffer
}

func newEmailEnv(t *testing.T) *emailEnv {
	t.Helper()
	env := &emailEnv{
		users:    newMem(),
		tokens:   newMemTokens(),
		mail:     &mailertest.Capture{},
		sessions: &fakeRevoker{},
		now:      time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
		logs:     &bytes.Buffer{},
	}
	svc, err := NewService(env.users, Options{
		Hash:   fast,
		Now:    func() time.Time { return env.now },
		Tokens: env.tokens, Mail: env.mail, Sessions: env.sessions,
		BaseURL: "https://tierl.example/", Lang: mailer.LangRU,
		Log: slog.New(slog.NewTextHandler(env.logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	env.svc = svc
	return env
}

func (e *emailEnv) advance(d time.Duration) { e.now = e.now.Add(d) }

const goodPassword = "correct-horse-battery"

func (e *emailEnv) register(t *testing.T, email string) *User {
	t.Helper()
	u, err := e.svc.Register(context.Background(), RegisterInput{
		Email: email, Password: goodPassword, DisplayName: "Анна",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var tokenRE = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func tokenFrom(t *testing.T, m mailer.Message) string {
	t.Helper()
	for _, body := range []string{m.Text, m.HTML} {
		if sub := tokenRE.FindStringSubmatch(body); sub != nil {
			return sub[1]
		}
	}
	t.Fatalf("no token in letter %q", m.Text)
	return ""
}

func TestVerifyEmailFlow(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	u := env.register(t, "Anna@Example.com")
	if env.mail.Len() != 1 {
		t.Fatalf("registration must send a confirmation letter, got %d", env.mail.Len())
	}
	msg, _ := env.mail.Last()
	if msg.To != "anna@example.com" || msg.Subject != "Подтвердите email в Tierl" {
		t.Fatalf("letter: %+v", msg)
	}
	token := tokenFrom(t, msg)
	if !strings.Contains(msg.Text, "https://tierl.example/verify-email?token=") {
		t.Fatalf("wrong link: %s", msg.Text)
	}
	if u.EmailVerified {
		t.Fatal("a new user must not be verified")
	}
	if err := env.svc.VerifyEmail(ctx, token); err != nil {
		t.Fatal(err)
	}
	got, _ := env.users.GetByID(ctx, u.ID)
	if !got.EmailVerified {
		t.Fatal("email_verified not set")
	}
	if err := env.svc.VerifyEmail(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reused token: %v", err)
	}
	if err := env.svc.VerifyEmail(ctx, "forged"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("forged token: %v", err)
	}
	shape := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if err := env.svc.VerifyEmail(ctx, shape); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token: %v", err)
	}
	if env.logs.Len() != 0 {
		t.Fatalf("successful flow must not log: %s", env.logs)
	}
}

func TestVerifyEmailExpired(t *testing.T) {
	env := newEmailEnv(t)
	env.register(t, "anna@example.com")
	msg, _ := env.mail.Last()
	token := tokenFrom(t, msg)
	env.advance(VerifyEmailTTL)
	if err := env.svc.VerifyEmail(context.Background(), token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestResendInvalidatesPreviousToken(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	env.register(t, "anna@example.com")
	first, _ := env.mail.Last()
	if err := env.svc.ResendVerification(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	if env.mail.Len() != 2 {
		t.Fatalf("resend must send a second letter, got %d", env.mail.Len())
	}
	second, _ := env.mail.Last()
	if err := env.svc.VerifyEmail(ctx, tokenFrom(t, first)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old token must be invalidated: %v", err)
	}
	if err := env.svc.VerifyEmail(ctx, tokenFrom(t, second)); err != nil {
		t.Fatalf("new token: %v", err)
	}
	// A verified address gets no more letters.
	if err := env.svc.ResendVerification(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	if env.mail.Len() != 2 {
		t.Fatalf("verified user must not receive another letter, got %d", env.mail.Len())
	}
}

func TestResendUnknownEmailIsSilent(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	if err := env.svc.ResendVerification(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("unknown email must not error: %v", err)
	}
	if env.mail.Len() != 0 {
		t.Fatalf("unknown email must not receive mail, got %d", env.mail.Len())
	}
	if err := env.svc.ResendVerification(ctx, "not-an-email"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed email: %v", err)
	}
}

func TestForgotPasswordEnumeration(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	env.register(t, "anna@example.com")
	before := env.mail.Len()

	if err := env.svc.ForgotPassword(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("unknown email must not error: %v", err)
	}
	if env.mail.Len() != before {
		t.Fatal("unknown email must not trigger a letter")
	}
	if err := env.svc.ForgotPassword(ctx, "broken"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed email: %v", err)
	}
	if err := env.svc.ForgotPassword(ctx, "ANNA@example.com"); err != nil {
		t.Fatal(err)
	}
	if env.mail.Len() != before+1 {
		t.Fatalf("known email must get a letter, got %d", env.mail.Len())
	}
}

func TestResetPasswordFlow(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	u := env.register(t, "anna@example.com")
	verifyMsg, _ := env.mail.Last()
	verifyToken := tokenFrom(t, verifyMsg)

	if err := env.svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	resetMsg, _ := env.mail.Last()
	if !strings.Contains(resetMsg.Text, "/reset-password?token=") || resetMsg.Subject != "Сброс пароля в Tierl" {
		t.Fatalf("reset letter: %+v", resetMsg)
	}
	token := tokenFrom(t, resetMsg)

	if err := env.svc.ResetPassword(ctx, token, "another-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Login(ctx, "anna@example.com", "another-long-password"); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if _, err := env.svc.Login(ctx, "anna@example.com", goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password must stop working: %v", err)
	}
	if env.sessions.calls != 1 || env.sessions.user != u.ID {
		t.Fatalf("sessions must be revoked: %+v", env.sessions)
	}
	if n := env.tokens.active(u.ID, PurposeResetPassword); n != 0 {
		t.Fatalf("%d reset tokens left active", n)
	}
	if err := env.svc.ResetPassword(ctx, token, "yet-another-password"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reused reset token: %v", err)
	}
	// Purposes do not interfere: the confirmation link still works.
	if err := env.svc.VerifyEmail(ctx, verifyToken); err != nil {
		t.Fatalf("verify token after reset: %v", err)
	}
}

func TestResetPasswordValidationKeepsToken(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	env.register(t, "anna@example.com")
	if err := env.svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	msg, _ := env.mail.Last()
	token := tokenFrom(t, msg)
	if err := env.svc.ResetPassword(ctx, token, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short password: %v", err)
	}
	if err := env.svc.ResetPassword(ctx, token, "long-enough-password"); err != nil {
		t.Fatalf("token must survive validation: %v", err)
	}
}

func TestForgotInvalidatesPreviousResetToken(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	u := env.register(t, "anna@example.com")
	if err := env.svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	first, _ := env.mail.Last()
	if err := env.svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	second, _ := env.mail.Last()
	if n := env.tokens.active(u.ID, PurposeResetPassword); n != 1 {
		t.Fatalf("active reset tokens = %d, want 1", n)
	}
	if err := env.svc.ResetPassword(ctx, tokenFrom(t, first), "another-long-password"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old reset token: %v", err)
	}
	if err := env.svc.ResetPassword(ctx, tokenFrom(t, second), "another-long-password"); err != nil {
		t.Fatalf("new reset token: %v", err)
	}
}

func TestEmailDeliveryFailureIsNotAnOracle(t *testing.T) {
	env := newEmailEnv(t)
	ctx := context.Background()
	env.register(t, "anna@example.com")
	env.mail.SetError(errors.New("smtp is down"))

	if err := env.svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatalf("forgot must stay silent on delivery failure: %v", err)
	}
	if err := env.svc.ResendVerification(ctx, "anna@example.com"); err != nil {
		t.Fatalf("resend must stay silent on delivery failure: %v", err)
	}
	logs := env.logs.String()
	if !strings.Contains(logs, "failed") {
		t.Fatalf("delivery failure must be logged: %s", logs)
	}
	if strings.Contains(logs, "token=") || strings.Contains(logs, "/reset-password") || strings.Contains(logs, "/verify-email") {
		t.Fatalf("logs leaked the token or link: %s", logs)
	}
	if !strings.Contains(logs, "smtp is down") {
		t.Fatalf("logs should carry the cause: %s", logs)
	}
}

func TestEmailFlowsNotConfigured(t *testing.T) {
	svc := newSvc(t, newMem())
	ctx := context.Background()
	if err := svc.VerifyEmail(ctx, "x"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("verify: %v", err)
	}
	if err := svc.ResendVerification(ctx, "a@b.c"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("resend: %v", err)
	}
	if err := svc.ForgotPassword(ctx, "a@b.c"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("forgot: %v", err)
	}
	if err := svc.ResetPassword(ctx, "x", goodPassword); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("reset: %v", err)
	}
}

func TestTokenHashIsStable(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	hash, ok := hashOneTimeToken(raw)
	if !ok {
		t.Fatal("valid token rejected")
	}
	want := sha256.Sum256([]byte(raw))
	if hex.EncodeToString(hash) != hex.EncodeToString(want[:]) {
		t.Fatal("token hash mismatch")
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 42), raw + "x", raw + "=", strings.Repeat("!", 43)} {
		if _, ok := hashOneTimeToken(bad); ok {
			t.Errorf("token %q must be rejected", bad)
		}
	}
}

// slowMailer simulates a slow SMTP server.
type slowMailer struct {
	delay time.Duration
	inner Mailer
}

func (m *slowMailer) Send(ctx context.Context, msg mailer.Message) error {
	select {
	case <-time.After(m.delay):
		return m.inner.Send(ctx, msg)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestForgotPasswordTimingDoesNotDependOnEmailExistence(t *testing.T) {
	const delay = 400 * time.Millisecond
	users := newMem()
	tokens := newMemTokens()
	capture := &mailertest.Capture{}
	async, err := mailer.NewAsync(&slowMailer{delay: delay, inner: capture}, mailer.AsyncConfig{
		Workers: 1, QueueSize: 8, Timeout: 5 * time.Second,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(users, Options{
		Hash: fast, Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) },
		Tokens: tokens, Mail: async, BaseURL: "https://tierl.example",
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(context.Background(), RegisterInput{
		Email: "anna@example.com", Password: goodPassword, DisplayName: "Анна",
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	start := time.Now()
	if err := svc.ForgotPassword(ctx, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	known := time.Since(start)
	start = time.Now()
	if err := svc.ForgotPassword(ctx, "nobody@example.com"); err != nil {
		t.Fatal(err)
	}
	unknown := time.Since(start)

	// The handler must not wait for SMTP: half the artificial SMTP delay is a
	// generous bound that still fails if delivery becomes synchronous.
	if known > delay/2 || unknown > delay/2 {
		t.Fatalf("forgot is synchronous: known=%s unknown=%s (smtp delay %s)", known, unknown, delay)
	}
	diff := known - unknown
	if diff < 0 {
		diff = -diff
	}
	if diff > delay/2 {
		t.Fatalf("timing reveals existence: known=%s unknown=%s", known, unknown)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := async.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if capture.Len() != 2 {
		t.Fatalf("captured %d letters, want 2", capture.Len())
	}
	if got := async.Stats(); got.Dropped != 0 {
		t.Fatalf("queue dropped letters: %+v", got)
	}
}

func TestVerifyEmailStoreErrors(t *testing.T) {
	t.Run("consume error", func(t *testing.T) {
		env := newEmailEnv(t)
		env.register(t, "anna@example.com")
		msg, _ := env.mail.Last()
		env.tokens.failOn, env.tokens.failErr = "consume", errors.New("db down")
		if err := env.svc.VerifyEmail(context.Background(), tokenFrom(t, msg)); err == nil {
			t.Fatal("consume error must be reported")
		}
	})
	t.Run("invalidate error", func(t *testing.T) {
		env := newEmailEnv(t)
		u := env.register(t, "anna@example.com")
		msg, _ := env.mail.Last()
		env.tokens.failOn, env.tokens.failErr = "invalidate", errors.New("db down")
		if err := env.svc.VerifyEmail(context.Background(), tokenFrom(t, msg)); err == nil {
			t.Fatal("invalidate error must be reported")
		}
		if got, _ := env.users.GetByID(context.Background(), u.ID); got.EmailVerified {
			t.Fatal("email must not be marked verified when cleanup failed")
		}
	})
	t.Run("mark error", func(t *testing.T) {
		env := newEmailEnv(t)
		raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
		hash, _ := hashOneTimeToken(raw)
		if err := env.tokens.Issue(context.Background(), &OneTimeToken{
			ID: "ghost-token", UserID: "ghost", Purpose: PurposeVerifyEmail, TokenHash: hash,
			CreatedAt: env.now, ExpiresAt: env.now.Add(VerifyEmailTTL),
		}); err != nil {
			t.Fatal(err)
		}
		if err := env.svc.VerifyEmail(context.Background(), raw); err == nil {
			t.Fatal("unknown user must be reported")
		}
	})
}

func TestResetPasswordPropagatesErrors(t *testing.T) {
	setup := func(t *testing.T) (*emailEnv, string) {
		t.Helper()
		env := newEmailEnv(t)
		env.register(t, "anna@example.com")
		if err := env.svc.ForgotPassword(context.Background(), "anna@example.com"); err != nil {
			t.Fatal(err)
		}
		msg, _ := env.mail.Last()
		return env, tokenFrom(t, msg)
	}

	t.Run("revoke sessions error", func(t *testing.T) {
		env, token := setup(t)
		env.sessions.err = errors.New("sessions db down")
		if err := env.svc.ResetPassword(context.Background(), token, "another-long-password"); err == nil {
			t.Fatal("revocation error must fail the reset")
		}
		if _, err := env.svc.Login(context.Background(), "anna@example.com", goodPassword); err != nil {
			t.Fatalf("password must stay unchanged: %v", err)
		}
	})
	t.Run("invalidate tokens error", func(t *testing.T) {
		env, token := setup(t)
		env.tokens.failOn, env.tokens.failErr = "invalidate", errors.New("db down")
		if err := env.svc.ResetPassword(context.Background(), token, "another-long-password"); err == nil {
			t.Fatal("invalidate error must fail the reset")
		}
		if _, err := env.svc.Login(context.Background(), "anna@example.com", goodPassword); err != nil {
			t.Fatalf("password must stay unchanged: %v", err)
		}
	})
	t.Run("update hash error", func(t *testing.T) {
		env, token := setup(t)
		env.users.failUpd = errors.New("db down")
		if err := env.svc.ResetPassword(context.Background(), token, "another-long-password"); err == nil {
			t.Fatal("update error must fail the reset")
		}
		if env.sessions.calls != 1 {
			t.Fatalf("sessions must be revoked before the failed update, calls=%d", env.sessions.calls)
		}
	})
	t.Run("consume error", func(t *testing.T) {
		env, token := setup(t)
		env.tokens.failOn, env.tokens.failErr = "consume", errors.New("db down")
		if err := env.svc.ResetPassword(context.Background(), token, "another-long-password"); err == nil {
			t.Fatal("consume error must be reported")
		}
	})
	t.Run("hash error", func(t *testing.T) {
		env, token := setup(t)
		env.svc.hasher.rand = failingReader{}
		if err := env.svc.ResetPassword(context.Background(), token, "another-long-password"); err == nil {
			t.Fatal("hash error must be reported")
		}
		if _, err := env.svc.Login(context.Background(), "anna@example.com", goodPassword); err != nil {
			t.Fatalf("password must stay unchanged: %v", err)
		}
	})
}

func TestIssueAndSendErrorsAreContained(t *testing.T) {
	t.Run("token store failure during register", func(t *testing.T) {
		env := newEmailEnv(t)
		env.tokens.failOn, env.tokens.failErr = "issue", errors.New("db down")
		if _, err := env.svc.Register(context.Background(), RegisterInput{
			Email: "anna@example.com", Password: goodPassword, DisplayName: "Анна",
		}); err != nil {
			t.Fatalf("registration must succeed without the letter: %v", err)
		}
		if env.mail.Len() != 0 {
			t.Fatal("no letter must be sent")
		}
		if !strings.Contains(env.logs.String(), "send verification email failed") {
			t.Fatalf("failure not logged: %s", env.logs)
		}
	})
	t.Run("token generation failure during forgot", func(t *testing.T) {
		env := newEmailEnv(t)
		env.register(t, "anna@example.com")
		env.svc.rand = failingReader{}
		if err := env.svc.ForgotPassword(context.Background(), "anna@example.com"); err != nil {
			t.Fatal("forgot must stay silent")
		}
		if !strings.Contains(env.logs.String(), "forgot password failed") {
			t.Fatalf("failure not logged: %s", env.logs)
		}
	})
	t.Run("id generation failure during forgot", func(t *testing.T) {
		env := newEmailEnv(t)
		env.register(t, "anna@example.com")
		env.svc.rand = io.LimitReader(rand.Reader, 32)
		if err := env.svc.ForgotPassword(context.Background(), "anna@example.com"); err != nil {
			t.Fatal("forgot must stay silent")
		}
		if !strings.Contains(env.logs.String(), "forgot password failed") {
			t.Fatalf("failure not logged: %s", env.logs)
		}
	})
	t.Run("user lookup failure", func(t *testing.T) {
		env := newEmailEnv(t)
		env.users.failGet = errors.New("db down")
		if err := env.svc.ForgotPassword(context.Background(), "anna@example.com"); err == nil {
			t.Fatal("a database error is not an enumeration signal and must be reported")
		}
		if err := env.svc.ResendVerification(context.Background(), "anna@example.com"); err == nil {
			t.Fatal("resend must report a database error")
		}
	})
}

func TestInvalidBaseURLStillRendersRelativeLink(t *testing.T) {
	env := newEmailEnv(t)
	env.register(t, "anna@example.com")
	svc, err := NewService(env.users, Options{
		Hash: fast, Now: func() time.Time { return env.now },
		Tokens: env.tokens, Mail: env.mail, BaseURL: "://bad",
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ForgotPassword(context.Background(), "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	msg, _ := env.mail.Last()
	if !strings.Contains(msg.Text, "/reset-password?token=") {
		t.Fatalf("link missing: %s", msg.Text)
	}
}

func TestResetPasswordWithoutSessionStore(t *testing.T) {
	env := newEmailEnv(t)
	env.register(t, "anna@example.com")
	svc, err := NewService(env.users, Options{
		Hash: fast, Now: func() time.Time { return env.now },
		Tokens: env.tokens, Mail: env.mail,
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ForgotPassword(context.Background(), "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	msg, _ := env.mail.Last()
	if err := svc.ResetPassword(context.Background(), tokenFrom(t, msg), "another-long-password"); err != nil {
		t.Fatalf("reset without a session store: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }
