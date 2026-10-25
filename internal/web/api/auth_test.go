package api_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
	"github.com/basili4-1982/tierl/internal/web/api"
)

// stubAccounts records calls and returns configured errors.
type stubAccounts struct {
	verifyTokens   []string
	resetTokens    []string
	resetPasswords []string
	resendEmails   []string
	forgotEmails   []string

	verifyErr, resendErr, forgotErr, resetErr error
}

func (s *stubAccounts) VerifyEmail(_ context.Context, token string) error {
	s.verifyTokens = append(s.verifyTokens, token)
	return s.verifyErr
}

func (s *stubAccounts) ResendVerification(_ context.Context, email string) error {
	s.resendEmails = append(s.resendEmails, email)
	return s.resendErr
}

func (s *stubAccounts) ForgotPassword(_ context.Context, email string) error {
	s.forgotEmails = append(s.forgotEmails, email)
	return s.forgotErr
}

func (s *stubAccounts) ResetPassword(_ context.Context, token, newPassword string) error {
	s.resetTokens = append(s.resetTokens, token)
	s.resetPasswords = append(s.resetPasswords, newPassword)
	return s.resetErr
}

func newAuthHandler(t *testing.T, accounts api.Accounts, forgotLimit, resendLimit api.Middleware) http.Handler {
	t.Helper()
	return api.New(api.Deps{
		Accounts:                accounts,
		Log:                     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ForgotPasswordLimit:     forgotLimit,
		ResendVerificationLimit: resendLimit,
	})
}

func authCall(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, api.Prefix+path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestVerifyEmailHTTP(t *testing.T) {
	stub := &stubAccounts{}
	h := newAuthHandler(t, stub, nil, nil)

	rec := authCall(t, h, "/auth/verify-email", `{"token":"abc"}`)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("verify = %d %q", rec.Code, rec.Body.String())
	}
	if len(stub.verifyTokens) != 1 || stub.verifyTokens[0] != "abc" {
		t.Fatalf("tokens = %v", stub.verifyTokens)
	}

	stub.verifyErr = auth.ErrInvalidToken
	if rec := authCall(t, h, "/auth/verify-email", `{"token":"expired"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid token = %d", rec.Code)
	} else if m := decodeMap(t, rec); m["code"] != "invalid_token" {
		t.Fatalf("code = %v", m["code"])
	}

	stub.verifyErr = errors.New("boom")
	wantError(t, authCall(t, h, "/auth/verify-email", `{"token":"x"}`), http.StatusInternalServerError, "internal_error")
}

func TestForgotPasswordHTTPAlwaysAccepted(t *testing.T) {
	stub := &stubAccounts{}
	h := newAuthHandler(t, stub, nil, nil)
	for _, email := range []string{"anna@example.com", "nobody@example.com"} {
		rec := authCall(t, h, "/auth/forgot-password", `{"email":"`+email+`"}`)
		if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
			t.Fatalf("forgot %s = %d %q", email, rec.Code, rec.Body.String())
		}
	}
	if len(stub.forgotEmails) != 2 {
		t.Fatalf("calls = %v", stub.forgotEmails)
	}

	stub.forgotErr = &auth.ValidationError{Field: "email", Reason: "not a valid address"}
	wantError(t, authCall(t, h, "/auth/forgot-password", `{"email":"bad"}`), http.StatusUnprocessableEntity, "validation_failed")

	stub.forgotErr = errors.New("db is down")
	wantError(t, authCall(t, h, "/auth/forgot-password", `{"email":"a@b.c"}`), http.StatusInternalServerError, "internal_error")
}

func TestResendVerificationHTTP(t *testing.T) {
	stub := &stubAccounts{}
	h := newAuthHandler(t, stub, nil, nil)
	for _, email := range []string{"anna@example.com", "nobody@example.com"} {
		if rec := authCall(t, h, "/auth/resend-verification", `{"email":"`+email+`"}`); rec.Code != http.StatusAccepted {
			t.Fatalf("resend %s = %d", email, rec.Code)
		}
	}
	stub.resendErr = auth.ErrNotConfigured
	wantError(t, authCall(t, h, "/auth/resend-verification", `{"email":"a@b.c"}`), http.StatusInternalServerError, "internal_error")
}

func TestResetPasswordHTTP(t *testing.T) {
	stub := &stubAccounts{}
	h := newAuthHandler(t, stub, nil, nil)

	rec := authCall(t, h, "/auth/reset-password", `{"token":"tok","new_password":"another-long-password"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %q", rec.Code, rec.Body.String())
	}
	if len(stub.resetTokens) != 1 || stub.resetPasswords[0] != "another-long-password" {
		t.Fatalf("calls: %v %v", stub.resetTokens, stub.resetPasswords)
	}

	stub.resetErr = auth.ErrInvalidToken
	if rec := authCall(t, h, "/auth/reset-password", `{"token":"bad","new_password":"another-long-password"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid token = %d", rec.Code)
	}

	stub.resetErr = &auth.ValidationError{Field: "password", Reason: "length must be 10 to 128 characters"}
	rec = authCall(t, h, "/auth/reset-password", `{"token":"tok","new_password":"short"}`)
	wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed")
	if fields, _ := decodeMap(t, rec)["details"].(map[string]any); fields["fields"] == nil {
		t.Fatalf("details.fields missing: %s", rec.Body.String())
	}
}

func TestAuthEndpointsRejectBadBodies(t *testing.T) {
	h := newAuthHandler(t, &stubAccounts{}, nil, nil)
	for _, path := range []string{"/auth/verify-email", "/auth/resend-verification", "/auth/forgot-password", "/auth/reset-password"} {
		if rec := authCall(t, h, path, `{`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: malformed = %d", path, rec.Code)
		}
		if rec := authCall(t, h, path, `{"unknown":"x"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: unknown field = %d", path, rec.Code)
		}
	}
}

func TestAuthRoutesAbsentWithoutAccounts(t *testing.T) {
	h := newAuthHandler(t, nil, nil, nil)
	for _, path := range []string{"/auth/verify-email", "/auth/forgot-password"} {
		if rec := authCall(t, h, path, `{}`); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
}

func TestAuthRateLimits(t *testing.T) {
	limiter, err := ratelimit.NewMemory(ratelimit.Config{Burst: 1, Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	limit := ratelimit.Middleware(limiter, ratelimit.IPKey([]byte("salt"), "auth_email"))
	h := newAuthHandler(t, &stubAccounts{}, limit, limit)

	if rec := authCall(t, h, "/auth/forgot-password", `{"email":"anna@example.com"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("first = %d", rec.Code)
	}
	rec := authCall(t, h, "/auth/forgot-password", `{"email":"anna@example.com"}`)
	wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing")
	}
	if rec := authCall(t, h, "/auth/resend-verification", `{"email":"anna@example.com"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("resend limit = %d", rec.Code)
	}
}
