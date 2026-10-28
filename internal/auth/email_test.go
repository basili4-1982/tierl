package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/platform/mailer"
	"code.sarnas.ru/tierl/internal/platform/mailer/mailertest"
)

type memTokens struct {
	mu     sync.Mutex
	byHash map[string]*OneTimeToken
}

func newMemTokens() *memTokens {
	return &memTokens{byHash: map[string]*OneTimeToken{}}
}

func (m *memTokens) Issue(_ context.Context, t *OneTimeToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.byHash {
		if old.UserID == t.UserID && old.Purpose == t.Purpose && old.UsedAt == nil {
			used := t.CreatedAt
			old.UsedAt = &used
		}
	}
	c := *t
	c.TokenHash = append([]byte(nil), t.TokenHash...)
	m.byHash[string(c.TokenHash)] = &c
	return nil
}

func (m *memTokens) Consume(_ context.Context, hash []byte, purpose Purpose, now time.Time) (*OneTimeToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byHash[string(hash)]
	if !ok || t.Purpose != purpose || t.UsedAt != nil || !t.ExpiresAt.After(now) {
		return nil, ErrInvalidToken
	}
	used := now
	t.UsedAt = &used
	c := *t
	c.TokenHash = append([]byte(nil), t.TokenHash...)
	return &c, nil
}

func (m *memTokens) Invalidate(_ context.Context, userID string, purpose Purpose, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.byHash {
		if t.UserID == userID && t.Purpose == purpose && t.UsedAt == nil {
			used := now
			t.UsedAt = &used
		}
	}
	return nil
}

type emailFixture struct {
	users  *memUsers
	tokens *memTokens
	mail   *mailertest.Capture
	now    time.Time
	svc    *Service
}

func newEmailFixture(t *testing.T) *emailFixture {
	t.Helper()
	f := &emailFixture{
		users:  newMem(),
		tokens: newMemTokens(),
		mail:   &mailertest.Capture{},
		now:    time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}
	svc, err := NewService(f.users, Options{
		Hash:    fast,
		Now:     func() time.Time { return f.now },
		Tokens:  f.tokens,
		Mail:    f.mail,
		BaseURL: "https://tierl.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func (f *emailFixture) register(t *testing.T) *User {
	t.Helper()
	u, err := f.svc.Register(context.Background(), RegisterInput{
		Email: "Anna@Example.COM", Password: goodPw, DisplayName: "Anna",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func verifyTokenFromMessage(t *testing.T, m mailer.Message) string {
	t.Helper()
	const prefix = "/verify-email?token="
	start := strings.Index(m.Text, prefix)
	if start < 0 {
		t.Fatalf("message has no verification link:\n%s", m.Text)
	}
	token := m.Text[start+len(prefix):]
	if end := strings.IndexAny(token, " \t\r\n\"'<>"); end >= 0 {
		token = token[:end]
	}
	if token == "" {
		t.Fatal("verification link has an empty token")
	}
	return token
}

func TestEmailVerificationFullFlow(t *testing.T) {
	f := newEmailFixture(t)
	ctx := context.Background()
	u := f.register(t)

	if u.EmailVerified {
		t.Fatal("registration must leave the email unverified")
	}
	if f.mail.Len() != 1 {
		t.Fatalf("registration sent %d messages, want 1", f.mail.Len())
	}
	msg, _ := f.mail.Last()
	if msg.To != "anna@example.com" {
		t.Fatalf("message recipient = %q", msg.To)
	}
	if !strings.Contains(msg.Text, "https://tierl.example/verify-email?token=") {
		t.Fatalf("message has no absolute verification link:\n%s", msg.Text)
	}

	token := verifyTokenFromMessage(t, msg)
	hash := sha256.Sum256([]byte(token))
	stored, ok := f.tokens.byHash[string(hash[:])]
	if !ok || len(stored.TokenHash) != sha256.Size {
		t.Fatal("the token hash is not stored")
	}
	if _, ok := f.tokens.byHash[token]; ok {
		t.Fatal("the raw token is stored in the token store")
	}

	if err := f.svc.VerifyEmail(ctx, token); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	verified, err := f.users.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.EmailVerified {
		t.Fatal("email was not marked verified")
	}

	if err := f.svc.VerifyEmail(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second use: got %v, want ErrInvalidToken", err)
	}
}

func TestVerifyEmailExpiredToken(t *testing.T) {
	f := newEmailFixture(t)
	u := f.register(t)
	msg, _ := f.mail.Last()
	token := verifyTokenFromMessage(t, msg)

	f.now = f.now.Add(VerifyEmailTTL + time.Second)
	if err := f.svc.VerifyEmail(context.Background(), token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token: got %v, want ErrInvalidToken", err)
	}
	got, err := f.users.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EmailVerified {
		t.Fatal("expired token must not verify the email")
	}
}

func TestResendVerificationInvalidatesPreviousToken(t *testing.T) {
	f := newEmailFixture(t)
	ctx := context.Background()
	u := f.register(t)
	first, _ := f.mail.Last()
	firstToken := verifyTokenFromMessage(t, first)

	f.now = f.now.Add(time.Minute)
	if err := f.svc.ResendVerification(ctx, "ANNA@example.com"); err != nil {
		t.Fatalf("resend verification: %v", err)
	}
	if f.mail.Len() != 2 {
		t.Fatalf("resend sent %d messages, want 2", f.mail.Len())
	}
	second, _ := f.mail.Last()
	secondToken := verifyTokenFromMessage(t, second)
	if secondToken == firstToken {
		t.Fatal("resend reused the previous token")
	}

	if err := f.svc.VerifyEmail(ctx, firstToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("previous token after resend: got %v, want ErrInvalidToken", err)
	}
	if err := f.svc.VerifyEmail(ctx, secondToken); err != nil {
		t.Fatalf("new token: %v", err)
	}
	got, err := f.users.GetByID(ctx, u.ID)
	if err != nil || !got.EmailVerified {
		t.Fatalf("email after resend confirmation = %+v, %v", got, err)
	}
}

func TestResendVerificationIsSilentForUnknownAndVerified(t *testing.T) {
	f := newEmailFixture(t)
	ctx := context.Background()
	u := f.register(t)

	if err := f.svc.ResendVerification(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("unknown email: %v", err)
	}
	if f.mail.Len() != 1 {
		t.Fatalf("unknown email sent a message: %d", f.mail.Len())
	}

	msg, _ := f.mail.Last()
	if err := f.svc.VerifyEmail(ctx, verifyTokenFromMessage(t, msg)); err != nil {
		t.Fatal(err)
	}
	before := f.mail.Len()
	if err := f.svc.ResendVerification(ctx, u.Email); err != nil {
		t.Fatalf("verified email: %v", err)
	}
	if f.mail.Len() != before {
		t.Fatalf("verified email sent another message: %d -> %d", before, f.mail.Len())
	}
}

func TestRegisterSurvivesDeliveryFailure(t *testing.T) {
	f := newEmailFixture(t)
	boom := errors.New("smtp unavailable")
	f.mail.SetError(boom)

	u, err := f.svc.Register(context.Background(), RegisterInput{
		Email: "bob@example.com", Password: goodPw, DisplayName: "Bob",
	})
	if err != nil {
		t.Fatalf("registration failed because of delivery: %v", err)
	}
	if u.EmailVerified {
		t.Fatal("new account must stay unverified")
	}
	if f.mail.Len() != 0 {
		t.Fatalf("failed delivery captured %d messages", f.mail.Len())
	}
}

func TestVerifyEmailRejectsMalformedToken(t *testing.T) {
	f := newEmailFixture(t)
	for _, token := range []string{"", "short", strings.Repeat("x", 43)} {
		if err := f.svc.VerifyEmail(context.Background(), token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %q: got %v, want ErrInvalidToken", token, err)
		}
	}
}
