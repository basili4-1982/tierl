package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/platform/mailer"
	"code.sarnas.ru/tierl/internal/platform/mailer/mailertest"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
)

var verificationLinkPattern = regexp.MustCompile(`https://tierl\.example/verify-email\?token=([A-Za-z0-9_-]+)`)

type emailTokens struct {
	mu     sync.Mutex
	tokens []*OneTimeToken
}

var _ TokenStore = (*emailTokens)(nil)

func (m *emailTokens) Issue(_ context.Context, t *OneTimeToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.tokens {
		if old.UserID == t.UserID && old.Purpose == t.Purpose && old.UsedAt == nil {
			used := t.CreatedAt
			old.UsedAt = &used
		}
	}
	cp := *t
	cp.TokenHash = append([]byte(nil), t.TokenHash...)
	m.tokens = append(m.tokens, &cp)
	return nil
}

func (m *emailTokens) Consume(_ context.Context, tokenHash []byte, purpose Purpose, now time.Time) (*OneTimeToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.Purpose != purpose || t.UsedAt != nil || !t.ExpiresAt.After(now) ||
			!bytes.Equal(t.TokenHash, tokenHash) {
			continue
		}
		used := now
		t.UsedAt = &used
		cp := *t
		cp.TokenHash = append([]byte(nil), t.TokenHash...)
		return &cp, nil
	}
	return nil, ErrInvalidToken
}

func (m *emailTokens) Invalidate(_ context.Context, userID string, purpose Purpose, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.UserID == userID && t.Purpose == purpose && t.UsedAt == nil {
			used := now
			t.UsedAt = &used
		}
	}
	return nil
}

type emailProfiles struct {
	users *memUsers
}

func (p emailProfiles) Profile(ctx context.Context, id string) (tierlist.Profile, error) {
	u, err := p.users.GetByID(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		return tierlist.Profile{}, tierlist.ErrNotFound
	}
	if err != nil {
		return tierlist.Profile{}, err
	}
	return tierlist.Profile{DisplayName: u.DisplayName, EmailVerified: u.EmailVerified}, nil
}

func newEmailSvc(t *testing.T, users *memUsers, tokens TokenStore, mail *mailertest.Capture, now func() time.Time) *Service {
	t.Helper()
	svc, err := NewService(users, Options{
		Hash: fast, Now: now, Tokens: tokens, Mail: mail,
		BaseURL: "https://tierl.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func verificationToken(t *testing.T, msg mailer.Message) string {
	t.Helper()
	for _, body := range []string{msg.Text, msg.HTML} {
		if match := verificationLinkPattern.FindStringSubmatch(body); len(match) == 2 {
			return match[1]
		}
	}
	t.Fatalf("verification email has no confirmation link: %q", msg.Text)
	return ""
}

func TestEmailVerificationFullPath(t *testing.T) {
	ctx := context.Background()
	users := newMem()
	tokens := &emailTokens{}
	mail := &mailertest.Capture{}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc := newEmailSvc(t, users, tokens, mail, clock)

	u, err := svc.Register(ctx, RegisterInput{
		Email: "anna@example.com", Password: goodPw, DisplayName: "Anna",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.EmailVerified {
		t.Fatal("registration must not verify the email")
	}
	if mail.Len() != 1 {
		t.Fatalf("registration sent %d emails, want 1", mail.Len())
	}
	msg, ok := mail.Last()
	if !ok {
		t.Fatal("registration did not deliver a verification email")
	}
	if msg.To != "anna@example.com" || msg.Subject == "" || msg.Text == "" || msg.HTML == "" {
		t.Fatalf("unexpected verification message: %+v", msg)
	}
	token := verificationToken(t, msg)
	if want := base64.RawURLEncoding.EncodedLen(oneTimeTokenBytes); len(token) != want {
		t.Fatalf("token has %d characters, want %d", len(token), want)
	}

	if len(tokens.tokens) != 1 {
		t.Fatalf("stored %d tokens, want 1", len(tokens.tokens))
	}
	stored := tokens.tokens[0]
	if stored.Purpose != PurposeVerifyEmail || stored.UserID != u.ID {
		t.Fatalf("unexpected stored token: %+v", stored)
	}
	if !stored.ExpiresAt.Equal(now.Add(VerifyEmailTTL)) {
		t.Fatalf("token expires at %v, want %v", stored.ExpiresAt, now.Add(VerifyEmailTTL))
	}
	hash := sha256.Sum256([]byte(token))
	if !bytes.Equal(stored.TokenHash, hash[:]) {
		t.Fatal("stored token hash is not SHA-256 of the raw token")
	}
	if bytes.Contains(stored.TokenHash, []byte(token)) {
		t.Fatal("raw token is stored instead of its hash")
	}

	listStore := tierlisttest.NewMemStore()
	lists := tierlist.NewService(listStore, emailProfiles{users: users}, tierlist.ServiceDeps{Now: clock})
	input := tierlist.CreateInput{
		Title:       "Email confirmation",
		Description: "created after following the confirmation link",
		Visibility:  domain.VisibilityPublic,
		Slug:        "email-confirmation",
		Tiers:       []tierlist.TierInput{{Name: "S", Color: "#ff0000"}, {Name: "A", Color: "#00ff00"}},
		Items:       []tierlist.ItemInput{{Label: "Go"}, {Label: "Rust"}, {Label: "Zig"}},
	}
	if _, err := lists.Create(ctx, u.ID, input); !errors.Is(err, tierlist.ErrEmailNotVerified) {
		t.Fatalf("create before confirmation: want ErrEmailNotVerified, got %v", err)
	}

	if err := svc.VerifyEmail(ctx, token); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	verified, err := users.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.EmailVerified {
		t.Fatal("email is still unverified after following the token")
	}

	created, err := lists.Create(ctx, u.ID, input)
	if err != nil {
		t.Fatalf("create after confirmation: %v", err)
	}
	if created.Tierlist.Slug != input.Slug || created.Tierlist.AuthorID != u.ID {
		t.Fatalf("created tier list = %+v", created.Tierlist)
	}
	if _, err := listStore.GetBySlug(ctx, input.Slug); err != nil {
		t.Fatalf("created tier list is not stored: %v", err)
	}

	if err := svc.VerifyEmail(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second use of the token: want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyEmailRejectsInvalidTokens(t *testing.T) {
	ctx := context.Background()
	users := newMem()
	tokens := &emailTokens{}
	mail := &mailertest.Capture{}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc := newEmailSvc(t, users, tokens, mail, clock)

	raw, _, err := newOneTimeToken(strings.NewReader(strings.Repeat("k", oneTimeTokenBytes)))
	if err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{
		"empty":         "",
		"short":         "abc",
		"too long":      raw + "x",
		"invalid chars": strings.Repeat("!", len(raw)),
		"unknown":       raw,
	} {
		t.Run(name, func(t *testing.T) {
			if err := svc.VerifyEmail(ctx, token); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("want ErrInvalidToken, got %v", err)
			}
		})
	}

	u, err := svc.Register(ctx, RegisterInput{Email: "anna@example.com", Password: goodPw, DisplayName: "Anna"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := mail.Last()
	token := verificationToken(t, msg)

	now = now.Add(VerifyEmailTTL + time.Second)
	if err := svc.VerifyEmail(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token: want ErrInvalidToken, got %v", err)
	}
	if got, err := users.GetByID(ctx, u.ID); err != nil || got.EmailVerified {
		t.Fatalf("expired token verified the user: %+v %v", got, err)
	}
}

func TestVerifyEmailRequiresTokenStore(t *testing.T) {
	svc := newSvc(t, newMem())
	raw, _, err := newOneTimeToken(strings.NewReader(strings.Repeat("k", oneTimeTokenBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(context.Background(), raw); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestResendVerificationInvalidatesPreviousToken(t *testing.T) {
	ctx := context.Background()
	users := newMem()
	tokens := &emailTokens{}
	mail := &mailertest.Capture{}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	svc := newEmailSvc(t, users, tokens, mail, func() time.Time { return now })

	u, err := svc.Register(ctx, RegisterInput{Email: "anna@example.com", Password: goodPw, DisplayName: "Anna"})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := mail.Last()
	firstToken := verificationToken(t, first)

	if err := svc.ResendVerification(ctx, "ANNA@example.com"); err != nil {
		t.Fatal(err)
	}
	if mail.Len() != 2 {
		t.Fatalf("resend sent %d emails, want 2 total", mail.Len())
	}
	second, _ := mail.Last()
	secondToken := verificationToken(t, second)
	if firstToken == secondToken {
		t.Fatal("resend reused the previous token")
	}

	if err := svc.VerifyEmail(ctx, firstToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("previous token: want ErrInvalidToken, got %v", err)
	}
	if err := svc.VerifyEmail(ctx, secondToken); err != nil {
		t.Fatalf("new token: %v", err)
	}

	before := mail.Len()
	if err := svc.ResendVerification(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	if mail.Len() != before {
		t.Fatal("resend sent another email to an already verified address")
	}
	if err := svc.ResendVerification(ctx, "nobody@example.com"); err != nil {
		t.Fatal(err)
	}
	if mail.Len() != before {
		t.Fatal("resend sent an email to an unknown address")
	}
	if err := svc.ResendVerification(ctx, "not-an-email"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed email: want ErrInvalid, got %v", err)
	}
}

func TestRegisterSurvivesDeliveryFailure(t *testing.T) {
	ctx := context.Background()
	users := newMem()
	tokens := &emailTokens{}
	mail := &mailertest.Capture{}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	svc := newEmailSvc(t, users, tokens, mail, func() time.Time { return now })
	mail.SetError(errors.New("smtp down"))

	u, err := svc.Register(ctx, RegisterInput{Email: "anna@example.com", Password: goodPw, DisplayName: "Anna"})
	if err != nil {
		t.Fatalf("registration must not fail when delivery fails: %v", err)
	}
	if _, err := users.GetByID(ctx, u.ID); err != nil {
		t.Fatalf("account was not created: %v", err)
	}
	if mail.Len() != 0 {
		t.Fatalf("captured %d failed messages, want 0", mail.Len())
	}
	if err := svc.ResendVerification(ctx, u.Email); err != nil {
		t.Fatalf("resend must hide delivery failures: %v", err)
	}
	if mail.Len() != 0 {
		t.Fatalf("captured %d failed messages, want 0", mail.Len())
	}
}
