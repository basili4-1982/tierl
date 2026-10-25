package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

func tokenHash(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

func oneTimeToken(id, userID string, purpose auth.Purpose, raw string, created time.Time, ttl time.Duration) *auth.OneTimeToken {
	return &auth.OneTimeToken{
		ID: id, UserID: userID, Purpose: purpose, TokenHash: tokenHash(raw),
		CreatedAt: created, ExpiresAt: created.Add(ttl),
	}
}

func newTokenStore(t *testing.T) *postgres.TokenStore {
	t.Helper()
	_, pool := newStore(t)
	store := postgres.NewTokenStore(pool)
	if err := postgres.NewUserStore(pool).Create(context.Background(), user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTokensIssueConsume(t *testing.T) {
	s := newTokenStore(t)
	ctx := context.Background()
	tok := oneTimeToken("t1", "u1", auth.PurposeVerifyEmail, "raw-token-1", t0, auth.VerifyEmailTTL)
	if err := s.Issue(ctx, tok); err != nil {
		t.Fatal(err)
	}
	got, err := s.Consume(ctx, tokenHash("raw-token-1"), auth.PurposeVerifyEmail, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "t1" || got.UserID != "u1" || got.Purpose != auth.PurposeVerifyEmail || got.UsedAt == nil {
		t.Fatalf("unexpected token: %+v", got)
	}
	// Single use.
	if _, err := s.Consume(ctx, tokenHash("raw-token-1"), auth.PurposeVerifyEmail, t0.Add(2*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("second consume: %v", err)
	}
	// A token of another purpose does not match.
	if _, err := s.Consume(ctx, tokenHash("raw-token-1"), auth.PurposeResetPassword, t0.Add(3*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("wrong purpose: %v", err)
	}
	// Unknown hash.
	if _, err := s.Consume(ctx, tokenHash("forged"), auth.PurposeVerifyEmail, t0); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("forged: %v", err)
	}
}

func TestTokensExpiry(t *testing.T) {
	s := newTokenStore(t)
	ctx := context.Background()
	if err := s.Issue(ctx, oneTimeToken("t1", "u1", auth.PurposeResetPassword, "raw", t0, auth.ResetPasswordTTL)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(ctx, tokenHash("raw"), auth.PurposeResetPassword, t0.Add(auth.ResetPasswordTTL)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("token must expire at its deadline: %v", err)
	}
	if _, err := s.Consume(ctx, tokenHash("raw"), auth.PurposeResetPassword, t0.Add(time.Minute)); err != nil {
		t.Fatalf("token must work before the deadline: %v", err)
	}
}

func TestTokenReissueInvalidatesPrevious(t *testing.T) {
	s := newTokenStore(t)
	ctx := context.Background()
	if err := s.Issue(ctx, oneTimeToken("t1", "u1", auth.PurposeVerifyEmail, "first", t0, auth.VerifyEmailTTL)); err != nil {
		t.Fatal(err)
	}
	if err := s.Issue(ctx, oneTimeToken("t2", "u1", auth.PurposeVerifyEmail, "second", t0.Add(time.Minute), auth.VerifyEmailTTL)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(ctx, tokenHash("first"), auth.PurposeVerifyEmail, t0.Add(2*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("old token: %v", err)
	}
	if _, err := s.Consume(ctx, tokenHash("second"), auth.PurposeVerifyEmail, t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("new token: %v", err)
	}
}

func TestTokenInvalidateKeepsOtherPurpose(t *testing.T) {
	s := newTokenStore(t)
	ctx := context.Background()
	if err := s.Issue(ctx, oneTimeToken("r1", "u1", auth.PurposeResetPassword, "reset", t0, auth.ResetPasswordTTL)); err != nil {
		t.Fatal(err)
	}
	if err := s.Issue(ctx, oneTimeToken("v1", "u1", auth.PurposeVerifyEmail, "verify", t0, auth.VerifyEmailTTL)); err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate(ctx, "u1", auth.PurposeResetPassword, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(ctx, tokenHash("reset"), auth.PurposeResetPassword, t0.Add(2*time.Minute)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("invalidated token: %v", err)
	}
	if _, err := s.Consume(ctx, tokenHash("verify"), auth.PurposeVerifyEmail, t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("other purpose must survive: %v", err)
	}
}

func TestTokenConcurrentConsume(t *testing.T) {
	s := newTokenStore(t)
	ctx := context.Background()
	if err := s.Issue(ctx, oneTimeToken("t1", "u1", auth.PurposeResetPassword, "race", t0, auth.ResetPasswordTTL)); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	ok := make(chan bool, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Consume(ctx, tokenHash("race"), auth.PurposeResetPassword, t0.Add(time.Minute))
			ok <- err == nil
		}()
	}
	wg.Wait()
	close(ok)
	wins := 0
	for won := range ok {
		if won {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d consumers redeemed the token, want 1", wins)
	}
}

func TestTokenIssueUnknownUser(t *testing.T) {
	s := newTokenStore(t)
	err := s.Issue(context.Background(), oneTimeToken("t1", "ghost", auth.PurposeVerifyEmail, "raw", t0, auth.VerifyEmailTTL))
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestTokenConcurrentIssueLeavesOneActive(t *testing.T) {
	_, pool := newStore(t)
	ctx := context.Background()
	if err := postgres.NewUserStore(pool).Create(ctx, user("u1", "anna@example.com")); err != nil {
		t.Fatal(err)
	}
	s := postgres.NewTokenStore(pool)
	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Issue(ctx, oneTimeToken(
				fmt.Sprintf("t%d", i), "u1", auth.PurposeVerifyEmail,
				fmt.Sprintf("raw-%d", i), t0, auth.VerifyEmailTTL))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent issue: %v", err)
		}
	}
	var active, total int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE used_at IS NULL), count(*) FROM one_time_tokens
		WHERE user_id = 'u1' AND purpose = 'verify_email'`).Scan(&active, &total); err != nil {
		t.Fatal(err)
	}
	if active != 1 || total != n {
		t.Fatalf("active=%d total=%d, want 1 and %d", active, total, n)
	}
}

func TestTokenStorePropagatesDatabaseErrors(t *testing.T) {
	s := newTokenStore(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Issue(canceled, oneTimeToken("t1", "u1", auth.PurposeVerifyEmail, "raw", t0, auth.VerifyEmailTTL)); err == nil {
		t.Error("Issue with a canceled context must fail")
	}
	if _, err := s.Consume(canceled, tokenHash("raw"), auth.PurposeVerifyEmail, t0); err == nil {
		t.Error("Consume with a canceled context must fail")
	}
	if err := s.Invalidate(canceled, "u1", auth.PurposeVerifyEmail, t0); err == nil {
		t.Error("Invalidate with a canceled context must fail")
	}
}

func TestTokenDuplicateHashIsRejected(t *testing.T) {
	s := newTokenStore(t)
	ctx := context.Background()
	if err := s.Issue(ctx, oneTimeToken("t1", "u1", auth.PurposeVerifyEmail, "dup", t0, auth.VerifyEmailTTL)); err != nil {
		t.Fatal(err)
	}
	// The raw hash is globally unique even though the previous token is now used.
	if err := s.Issue(ctx, oneTimeToken("t2", "u1", auth.PurposeVerifyEmail, "dup", t0.Add(time.Minute), auth.VerifyEmailTTL)); err == nil {
		t.Fatal("a duplicated token hash must violate the unique index")
	}
}
